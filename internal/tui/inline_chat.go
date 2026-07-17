package tui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// headerIdle is the header text when no turn is running. It is intentionally
// blank - the bottom status line carries the τ tau · model · provider context,
// and the header slot is reused to show the working indicator during a turn.
const headerIdle = ""

// quitConfirmWindow is how long a second Ctrl+C is honored as "confirm quit"
// after the first. The status-bar hint's Duration must match this exactly so
// the hint never outlives (or undersells) the armed window.
const quitConfirmWindow = 800 * time.Millisecond

type activeToolBox struct {
	row  *taui.ToolRow
	box  *taui.Box
	tail *taui.TailLog
}

// toolTailLines is how many trailing lines of streamed tool output stay
// visible in a running tool's box. It collapses away (RemoveChild) once the
// tool resolves - it's a live peek, not part of the permanent scrollback
// record the resolved ToolRow line becomes.
const toolTailLines = 6

type inlineChat struct {
	engine *taui.TUI
	ctrl   *inlineCtrl

	header      *taui.Text
	stage       *taui.Container
	panels      *taui.Container
	promptSlot  *taui.Container
	input       *taui.LineInput
	completions *taui.Completions
	status      *taui.Text

	// topDivider/bottomDivider surround the input box. Their mode func (set
	// once at construction) re-resolves the current input mode - e.g. "!"
	// bash mode or an active agent command - on every render, so they always
	// agree with the leading-trigger coloring in inputEchoStyle without any
	// push-based update wiring.
	topDivider, bottomDivider *taui.Divider

	// panelsByID indexes async, plugin-pushed panels by host-qualified view id
	// (pluginName + ":" + View.ID) so RenderView/CloseView can replace/remove
	// them in place. Like activeTools, it's only ever mutated inside an
	// engine.Update callback, so the engine's render lock is what serializes
	// access - not mu.
	panelsByID map[string]taui.Component

	ctx     context.Context
	runtime tauchat.ChatRuntime
	sub     *eventbus.Subscriber[tauchat.ChatEvent]
	usage   *metrics.UsageTracker
	done    chan struct{}

	bold, grey, dim func(string) string

	// Session/model/command state (ported from ChatPanel). Guarded by mu.
	provider          string
	refresh           ModelRefresher
	debug             bool
	notifyQueue       *notify.Queue
	sessionID         string
	modelName         string
	webURL            string
	showReasoning     bool
	reasoningEffort   string
	availableModels   []tauchat.ChatModelRef
	registryCommands  []tauchat.CommandRef
	extensionCommands map[string]tauchat.ExtensionCommand
	sessionSummaries  []tauchat.SessionSummary
	lastSubmit        time.Time
	// lastResponseText is the assistant's last fully-completed response, used
	// by /copy and its Ctrl+Shift+G shortcut. Set on ChatResponseCompletedEvent
	// (inline_events.go); empty until the first turn finishes.
	lastResponseText string

	// overlays routes input to whichever modal-like widget (interactive
	// prompt, completions dropdown) is currently active, ahead of the base
	// chat input. See taui.OverlayStack.
	overlays *taui.OverlayStack

	// promptQueue holds further interactive prompt requests raised by
	// plugins/tools via the host Confirm/Input API while one is already
	// showing. Only one prompt is shown at a time. Guarded by mu.
	promptQueue []tauchat.InteractivePromptRequestedEvent

	// Per-turn streaming state.
	mu            sync.Mutex
	turnText      *taui.Paragraph
	turnReasoning *taui.Paragraph
	activeTools   map[string]*activeToolBox // parallel tool boxes by CallID
	working       atomic.Bool
	generating    atomic.Bool // true while model generation is active
	steering      atomic.Bool
	running       bool
	queue         []string

	// Double Ctrl+C quit guard.
	pendingQuit time.Time
	cancelSent  bool

	// Bash-mode ("!cmd") state. Independent of generating/running - a bash
	// command executes outside the LLM turn loop and can run concurrently
	// with one. bashCallID identifies which activeTools entry (if any)
	// corresponds to the in-flight bash command, so its completion event can
	// clear bashRunning.
	bashRunning    atomic.Bool
	bashCancelSent bool
	bashCallID     string
}

func newInlineChat(
	ctx context.Context,
	engine *taui.TUI,
	runtime tauchat.ChatRuntime,
	sub *eventbus.Subscriber[tauchat.ChatEvent],
	usage *metrics.UsageTracker,
	cfg TUIConfig,
) *inlineChat {
	c := &inlineChat{
		engine:            engine,
		stage:             &taui.Container{},
		panels:            &taui.Container{},
		promptSlot:        &taui.Container{},
		panelsByID:        map[string]taui.Component{},
		ctx:               ctx,
		runtime:           runtime,
		sub:               sub,
		usage:             usage,
		done:              make(chan struct{}),
		provider:          cfg.Provider,
		refresh:           cfg.RefreshModels,
		debug:             cfg.Debug,
		notifyQueue:       notify.NewQueue(),
		sessionID:         cfg.SessionID,
		modelName:         cfg.ModelName,
		webURL:            cfg.WebURL,
		showReasoning:     cfg.ShowReasoning,
		reasoningEffort:   cfg.ReasoningEffort,
		availableModels:   slices.Clone(cfg.AvailableModels),
		registryCommands:  slices.Clone(cfg.InitialCommands),
		extensionCommands: map[string]tauchat.ExtensionCommand{},
		bold:              func(s string) string { return termkit.Wrap(s, termkit.Bold) },
		grey:              func(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) },
		dim:               func(s string) string { return termkit.Wrap(s, termkit.Dim, termkit.Italic) },
	}

	c.header = taui.NewStyledText(headerIdle, c.grey, nil)
	c.status = taui.NewText("")
	c.input = taui.NewLineInput("")
	c.input.SetStyles(c.bold, nil, c.grey)
	c.input.SetCommandStyle(inputEchoStyle)
	c.input.SetCommandCursorColor(theme.CommandFG)
	c.input.SetOnSubmit(c.onSubmit)

	c.completions = taui.NewCompletions(c.input, c.completionSet)
	c.completions.SetCursorColor(theme.CommandFG)
	c.completions.SetOnSelect(func(s string) {
		c.input.SetValueAndCursor(s, len([]rune(s)))
		c.engine.RequestRender()
	})
	c.completions.SetOnAccept(func(s string) {
		c.input.SetValueAndCursor(s, len([]rune(s)))
		value := c.input.Value()
		cur := c.input.Cursor()

		// If selecting this item leaves no further completions and we've
		// already moved past the command name into an argument slot, submit
		// immediately. Otherwise leave the filled line open so the user can
		// pick the next completion slot or keep typing.
		if c.argsBefore(value, cur) > 0 {
			set := c.completionSet(taui.CompletionContext{Text: value, Cursor: cur})
			if set == nil || len(set.Groups) == 0 {
				c.input.Clear()
				c.onSubmit(value)
				return
			}
		}
		c.engine.RequestRender()
	})
	c.completions.SetOnDetail(func(word string) bool {
		c.mu.Lock()
		for _, s := range c.sessionSummaries {
			if s.ID == word {
				c.mu.Unlock()
				c.printSessionInfo(s)
				return true
			}
		}
		c.mu.Unlock()
		return false
	})

	// The completions dropdown is a permanent soft overlay: it self-gates on
	// its own visibility, so pushing it once at construction and never
	// popping it is equivalent to (and replaces) the old hand-written
	// `if c.chat.completions.HandleInput(data) { ... }` branch.
	c.overlays = taui.NewOverlayStack(c.input)
	c.overlays.Push(c.completions, false)

	inputModeFn := func() *taui.InputMode { return currentInputMode(c.input.Value()) }
	c.topDivider = taui.NewDivider("")
	c.topDivider.SetModeFunc(inputModeFn)
	c.bottomDivider = taui.NewDivider("")
	c.bottomDivider.SetModeFunc(inputModeFn)
	c.bottomDivider.HideModeLabel() // only the top divider names the mode

	box := taui.NewBox().Padding(1, 1).ExpandW().Build()
	box.AddChild(c.header)
	box.AddChild(c.stage)
	box.AddChild(c.promptSlot)
	box.AddChild(c.topDivider)
	box.AddChild(c.input)
	box.AddChild(c.completions)
	// Panels render between the input area and the status bar - same region
	// the completions dropdown occupies, so they push the status line down
	// when present and let it snap back up when closed, exactly like
	// completions do. When empty, Container.Render returns nil, so there's
	// no dead space when no panels are open.
	box.AddChild(c.panels)
	box.AddChild(c.bottomDivider)
	box.AddChild(c.status)

	engine.AddChild(box)
	c.ctrl = &inlineCtrl{chat: c}
	engine.SetFocus(c.ctrl)

	go c.spinnerLoop()
	go c.statusLoop()
	go c.eventLoop()
	return c
}

func (c *inlineChat) close() {
	c.runtime.Close()
	close(c.done)
}

func (c *inlineChat) sid() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// send dispatches a command to the runtime, surfacing any error in scrollback.
func (c *inlineChat) send(cmd tauchat.ChatCommand) {
	if err := c.runtime.Send(cmd); err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), err.Error())
	}
}

// ── Spinner ─────────────────────────────────────────────────────────────────

func (c *inlineChat) spinnerLoop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.engine.Update(func() {
				// Tool-call rows only tick during a turn.
				if c.working.Load() {
					for _, tb := range c.activeTools {
						tb.row.Tick()
					}
				}
				// Async panel StatusRows tick whenever there are panels
				// mounted - they persist across turns, even while idle.
				for _, comp := range c.panelsByID {
					tickPanel(comp)
				}
			})
		}
	}
}

// ticker is the subset of taui.Component that can animate - both ToolRow
// and StatusRow implement it, and Tick is a no-op once resolved.
type ticker interface{ Tick() }

// tickPanel recursively walks a component tree and calls Tick on every node
// that implements the ticker interface.
func tickPanel(comp taui.Component) {
	if t, ok := comp.(ticker); ok {
		t.Tick()
	}
	if c, ok := comp.(*taui.Container); ok {
		for _, child := range c.Children {
			tickPanel(child)
		}
	}
	if s, ok := comp.(*taui.Stack); ok {
		for _, child := range s.Children {
			tickPanel(child)
		}
	}
	if b, ok := comp.(*taui.Box); ok {
		for _, child := range b.Children {
			tickPanel(child)
		}
	}
}

// ── Status line ──────────────────────────────────────────────────────────────

// statusLoop refreshes the bottom status line and drains the notification queue.
// It owns the engine.Update for the status text so every other path can simply
// mutate plain fields under mu without nesting render locks.
func (c *inlineChat) statusLoop() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			s := c.computeStatus()
			if s != last {
				last = s
				c.engine.Update(func() { c.status.SetText(s) })
			}
		}
	}
}

// computeStatus builds the two-group widget status bar: an identity group pinned
// left (brand · model · provider · effort) and a live-metrics group right-
// justified to the box's inner width. c.mu is taken once to snapshot fields, then
// released before touching the usage tracker or currentModelRef (both take their
// own locks - never nest).
func (c *inlineChat) computeStatus() string {
	c.mu.Lock()
	sid := c.sessionID
	model := c.modelName
	prov := c.provider
	effort := c.reasoningEffort
	webURL := c.webURL
	var note string
	if c.notifyQueue != nil {
		if cur := c.notifyQueue.Current(); cur != nil {
			note = cur.Message
		}
	}
	c.mu.Unlock()

	// Snapshot tracker totals without holding c.mu (tracker has its own lock).
	var totals *metrics.SessionTotals
	if c.usage != nil {
		totals = c.usage.Snapshot(sid)
	}
	// Context window from the selected model's config (takes c.mu internally).
	ctxWindow := c.currentModelRef().Config.ContextWindow

	// ── LEFT: identity (static during a turn). nil style → grey. ──
	left := []statusSeg{{text: "τ tau"}}
	if model != "" {
		left = append(left, statusSeg{text: model})
	}
	if prov != "" {
		left = append(left, statusSeg{text: prov})
	}
	if effort != "" && effort != "auto" {
		left = append(left, statusSeg{text: effort})
	}

	// ── RIGHT: live metrics (right-justified). Transient items lead so they
	// are never dropped by width pressure. ──
	var right []statusSeg
	if c.steering.Load() {
		right = append(right, statusSeg{text: c.steerLabel(), style: steerStyle, prio: prioTransient})
	}
	if note != "" {
		right = append(right, statusSeg{text: note, prio: prioTransient})
	}
	if totals != nil {
		if totals.PromptTokens > 0 || totals.CompletionTokens > 0 {
			right = append(right, statusSeg{
				text: "↑" + humanizeTokens(totals.PromptTokens) + " ↓" + humanizeTokens(totals.CompletionTokens),
				prio: prioTokens,
			})
		}
		if totals.Cost > 0 {
			right = append(right, statusSeg{text: formatCost(totals.Cost), prio: prioCost})
		}
		if p := contextPct(totals.LastPromptTokens, ctxWindow); p >= 0 {
			right = append(right, statusSeg{
				text:  formatContextPct(totals.LastPromptTokens, ctxWindow),
				style: contextStyle(p),
				prio:  prioContext,
			})
		}
	}
	if webURL != "" {
		right = append(right, statusSeg{
			text:           "Open Browser",
			styledOverride: termkit.FgOnly(termkit.Hyperlink("Open Browser", webURL), termkit.ColorGrey),
			prio:           prioWeb,
		})
	}

	// Box Padding(1,1) leaves an inner content width of termWidth-2.
	usable := max(c.width()-2, 1)
	return renderStatusBar(usable, left, right)
}

// steerLabel returns the plain animated [STEERING...] label, cycling the dot
// count every 300ms (driven by statusLoop's ticker). Style is applied separately
// via steerStyle so the navy colour doesn't bleed into following segments.
func (c *inlineChat) steerLabel() string {
	const label = "STEERING"
	return "[" + label + "." + strings.Repeat(".", int(time.Now().UnixMilli()/300)%3) + "]"
}

// steerStyle paints the steering indicator navy blue (no trailing reset; the
// next separator re-establishes grey).
func steerStyle(s string) string { return termkit.FgOnly(s, theme.SteeringFG) }

// pushNotice queues a transient info notification shown in the status line.
func (c *inlineChat) pushNotice(msg string) {
	c.mu.Lock()
	if c.notifyQueue != nil {
		c.notifyQueue.Push(notify.Notification{Message: msg, Level: notify.LevelInfo, Duration: 5 * time.Second})
	}
	c.mu.Unlock()
}

// ── Interactive prompts (plugin/tool Confirm/Input) ─────────────────────────

// enqueuePrompt shows the prompt immediately if no exclusive overlay (i.e.
// another prompt) is active, otherwise queues it behind the one currently
// shown.
func (c *inlineChat) enqueuePrompt(e tauchat.InteractivePromptRequestedEvent) {
	if c.overlays.HasExclusive() {
		c.mu.Lock()
		c.promptQueue = append(c.promptQueue, e)
		c.mu.Unlock()
		return
	}
	c.presentPrompt(e)
}

// presentPrompt builds a Prompt component for e, wires its callbacks to
// respond on the runtime, and pushes it as an exclusive overlay so it takes
// priority over the chat input and completions dropdown until resolved.
func (c *inlineChat) presentPrompt(e tauchat.InteractivePromptRequestedEvent) {
	var p *taui.Prompt
	switch e.Kind {
	case tauchat.InteractivePromptConfirm:
		p = taui.NewConfirmPrompt(e.Title, e.Message)
		p.SetOnConfirm(func(confirmed bool) {
			c.resolvePrompt(p, tauchat.RespondInteractivePromptCommand{
				RequestID: e.RequestID, Confirmed: confirmed, RespondedAt: time.Now().UTC(),
			})
		})
	default:
		p = taui.NewQuestionPrompt(e.Title, e.Message)
		p.SetOnAnswer(func(value string) {
			c.resolvePrompt(p, tauchat.RespondInteractivePromptCommand{
				RequestID: e.RequestID, Response: value, RespondedAt: time.Now().UTC(),
			})
		})
	}
	p.SetOnCancel(func() {
		c.resolvePrompt(p, tauchat.RespondInteractivePromptCommand{
			RequestID: e.RequestID, Canceled: true, RespondedAt: time.Now().UTC(),
		})
	})
	p.SetStyles(c.bold, c.grey)

	c.overlays.Push(p, true)
	c.engine.Update(func() {
		c.promptSlot.Clear()
		c.promptSlot.AddChild(p)
	})
}

// resolvePrompt sends the response to the runtime, pops p from the overlay
// stack, and shows the next queued prompt (if any).
func (c *inlineChat) resolvePrompt(p *taui.Prompt, resp tauchat.RespondInteractivePromptCommand) {
	c.send(resp)

	c.overlays.Pop(p)

	c.mu.Lock()
	var next *tauchat.InteractivePromptRequestedEvent
	if len(c.promptQueue) > 0 {
		e := c.promptQueue[0]
		c.promptQueue = c.promptQueue[1:]
		next = &e
	}
	c.mu.Unlock()

	c.engine.Update(func() { c.promptSlot.Clear() })
	if next != nil {
		c.presentPrompt(*next)
	}
}

// ── Submit / steer / queue ───────────────────────────────────────────────────

// onSubmit handles a completed input line. Slash commands are dispatched
// immediately; plain prompts start a turn (or queue behind a running one).
func (c *inlineChat) onSubmit(prompt string) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return
	}
	// Debounce rapid submits (double Enter, paste CR bytes).
	c.mu.Lock()
	if time.Since(c.lastSubmit) < 300*time.Millisecond {
		c.mu.Unlock()
		return
	}
	c.lastSubmit = time.Now()
	c.mu.Unlock()

	if strings.HasPrefix(trimmed, "/") {
		c.commit(inputEchoStyle(trimmed))
		c.handleSlashCommand(trimmed)
		return
	}

	if strings.HasPrefix(trimmed, "!") {
		c.commit(inputEchoStyle(trimmed))
		c.handleBashCommand(trimmed)
		return
	}

	c.startOrQueueTurn(trimmed)
}

// inputEchoStyle paints just the leading trigger character(s) of a slash
// command ("/") or bash-mode command ("!" / "!!") in an accent colour; the
// rest of the line (command name and arguments) is left in default styling.
// Used both for the live input (as each chunk of typed text is painted -
// only the chunk starting at the very beginning of the line will ever start
// with a trigger character) and the submitted echo into scrollback.
// currentInputMode resolves the input's current "mode" - bash ("!"/"!!") or
// an active agent command (e.g. "/plan") - if any. It is the single source
// of truth consulted by both inputEchoStyle (leading-trigger coloring) and
// the dividers surrounding the input box (see topDivider/bottomDivider), so
// all three always agree without any push-based update wiring - every
// render, each just asks "what mode is active right now?"
//
// Plain, non-agent slash commands (/model, /help, /session, ...) are
// intentionally NOT a "mode" - they're one-shot actions, not an operating
// mode change, so they resolve to nil here and keep the default accent.
func currentInputMode(s string) *taui.InputMode {
	trimmed := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(trimmed, "!"):
		if strings.HasPrefix(trimmed, "!!") {
			return &taui.InputMode{Label: "Shell (excluded)", Color: theme.BashExcludedFG}
		}
		return &taui.InputMode{Label: "Shell", Color: theme.BashFG}
	case strings.HasPrefix(trimmed, "/"):
		name, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "/"), " ")
		if cmd, ok := slashByName[strings.ToLower(name)]; ok && cmd.isAgent && cmd.mode != nil {
			return cmd.mode
		}
	}
	return nil
}

// inputEchoStyle paints the leading trigger character(s) of a slash command
// ("/") or bash-mode command ("!"/"!!") in an accent colour; the rest of the
// line is left in default styling. A "/" command that resolves to a
// configured input mode (see currentInputMode) - an agent command like
// "/plan" - uses that mode's colour instead of the default accent, so the
// trigger character always matches the surrounding dividers. Used both for
// live input (SetCommandStyle) and the submitted echo into scrollback.
func inputEchoStyle(s string) string {
	switch {
	case strings.HasPrefix(s, "!"):
		// Color the whole leading run of "!" (not just the first one or two),
		// matching how handleBashCommand strips it: 3+ bangs still mean
		// "excluded", and none of them should leak into the echoed line
		// looking like part of the command.
		bangs := len(s) - len(strings.TrimLeft(s, "!"))
		color := theme.BashFG
		if bangs >= 2 {
			color = theme.BashExcludedFG
		}
		return termkit.FgColor(s[:bangs], color) + s[bangs:]
	case strings.HasPrefix(s, "/"):
		color := theme.CommandFG
		if mode := currentInputMode(s); mode != nil {
			color = mode.Color
		}
		return termkit.FgColor("/", color) + s[1:]
	default:
		return s
	}
}

// startOrQueueTurn submits prompt as a new turn, or queues it behind a
// running one. Shared by onSubmit and agent-command activation (e.g. /plan
// with trailing text) so both interleave correctly with an in-flight turn.
func (c *inlineChat) startOrQueueTurn(prompt string) {
	c.input.AddToHistory(prompt)

	c.mu.Lock()
	if c.running {
		c.queue = append(c.queue, prompt)
		c.mu.Unlock()
		c.engine.PrintAbove("%s", c.grey("⏳ queued: "+prompt))
		return
	}
	c.running = true
	c.mu.Unlock()
	c.steering.Store(false)
	c.generating.Store(false)
	c.cancelSent = false
	go c.runTurn(prompt)
}

// onSteer handles Ctrl+S. When idle it's a normal submit. When busy it sends a
// SteerChatPromptCommand - the coordinator injects it after the next tool call
// completes (still within the same turn), so the agent course-corrects without
// losing progress.
func (c *inlineChat) onSteer(prompt string) {
	if strings.TrimSpace(prompt) == "" {
		return
	}
	if !c.working.Load() {
		c.onSubmit(prompt)
		return
	}
	c.engine.PrintAbove("%s %s", c.bold("⏎"), prompt)
	c.input.Clear()
	c.input.AddToHistory(prompt)
	c.steering.Store(true)
	_ = c.runtime.Send(tauchat.SteerChatPromptCommand{
		SessionID:   c.sid(),
		RequestID:   tauchat.NewRequestID(),
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	})
}

// handleBashCommand runs a "!" (or "!!") bash-mode command. Unlike slash
// commands, this executes outside the LLM turn loop - it can run
// concurrently with an in-flight chat turn - so it's guarded by its own
// bashRunning flag rather than working/generating. The CallID is generated
// here (not by the coordinator) so bashCallID can be recorded before the
// command is even sent, leaving no window where a ChatToolExecutionStartedEvent
// could arrive before the TUI knows which CallID is "ours."
func (c *inlineChat) handleBashCommand(trimmed string) {
	if c.bashRunning.Load() {
		c.engine.PrintAbove("%s", c.grey("bash command already running - press Esc to cancel"))
		return
	}

	exclude := strings.HasPrefix(trimmed, "!!")
	// Strip every leading "!", not just one or two - e.g. "!!!ls" (three
	// bangs) must not leave a literal "!" glued onto the front of the
	// command, which bash would otherwise choke on as a history-expansion
	// token.
	command := strings.TrimSpace(strings.TrimLeft(trimmed, "!"))
	if command == "" {
		return
	}

	c.input.AddToHistory(trimmed)

	callID := "bash-" + tauchat.NewRequestID()
	c.mu.Lock()
	c.bashCallID = callID
	c.bashCancelSent = false
	c.mu.Unlock()
	c.bashRunning.Store(true)

	if err := c.runtime.Send(tauchat.RunBashCommand{
		SessionID:   c.sid(),
		CallID:      callID,
		Command:     command,
		Exclude:     exclude,
		RequestedAt: time.Now().UTC(),
	}); err != nil {
		c.bashRunning.Store(false)
		c.mu.Lock()
		c.bashCallID = ""
		c.mu.Unlock()
		c.engine.PrintAbove("%s %s", c.grey("✗"), err.Error())
	}
}

func (c *inlineChat) runTurn(prompt string) {
	for {
		c.doTurn(prompt)
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.running = false
			c.mu.Unlock()
			return
		}
		prompt = c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
	}
}

func (c *inlineChat) doTurn(prompt string) {
	c.working.Store(true)
	c.cancelSent = false
	defer c.working.Store(false)

	c.mu.Lock()
	if c.notifyQueue != nil {
		if n := c.notifyQueue.Current(); n != nil && n.Level == notify.LevelError {
			c.notifyQueue.Dismiss()
		}
	}
	c.mu.Unlock()

	c.engine.Update(func() { c.header.SetText(c.grey("τ tau is working…")) })
	c.commit(c.bold("› " + prompt))

	err := c.runtime.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   c.sid(),
		RequestID:   tauchat.NewRequestID(),
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), err.Error())
		c.engine.Update(func() { c.header.SetText(c.grey(headerIdle)) })
	}
}

// cancelTurn sends a CancelChatRequestCommand to stop the current generation.
func (c *inlineChat) cancelTurn() {
	c.mu.Lock()
	if c.cancelSent {
		c.mu.Unlock()
		return
	}
	c.cancelSent = true
	c.mu.Unlock()

	c.steering.Store(false)
	c.engine.PrintAbove("%s", c.grey("cancelling…"))
	c.engine.Update(func() { c.header.SetText(c.grey(headerIdle)) })
	_ = c.runtime.Send(tauchat.CancelChatRequestCommand{
		SessionID:   c.sid(),
		RequestID:   "",
		RequestedAt: time.Now().UTC(),
	})
}

// cancelBash sends a CancelBashCommand to stop the in-flight "!" command.
func (c *inlineChat) cancelBash() {
	c.mu.Lock()
	if c.bashCancelSent {
		c.mu.Unlock()
		return
	}
	c.bashCancelSent = true
	c.mu.Unlock()

	_ = c.runtime.Send(tauchat.CancelBashCommand{
		SessionID:   c.sid(),
		RequestedAt: time.Now().UTC(),
	})
}

// ── Scrollback helpers ──────────────────────────────────────────────────────

func (c *inlineChat) commit(line string) {
	c.engine.PrintAbove("%s\n", line)
}

// seedHistoryFromMessages extracts user-role messages and seeds the input
// history buffer. Used when loading a persisted session.
func (c *inlineChat) seedHistoryFromMessages(messages []tauchat.ChatMessage) {
	var prompts []string
	for _, m := range messages {
		if m.Role == tauchat.ChatRoleUser && strings.TrimSpace(m.Content) != "" {
			prompts = append(prompts, m.Content)
		}
	}
	if len(prompts) > 0 {
		c.input.SetHistory(prompts)
	}
}

// blockString formats a rendered box (its lines) as a single scrollback string:
// each line gets a trailing SGR reset, and the block ends with a newline for
// spacing. Returned to UpdateThenPrint rather than printed directly so the
// commit happens outside the render lock.
func (c *inlineChat) blockString(lines []string) string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln + "\x1b[0m"
	}
	return strings.Join(out, "\n") + "\n"
}

func (c *inlineChat) width() int {
	w, _ := c.engine.Terminal.Size()
	if w <= 0 {
		w = 80
	}
	return w
}

// argsBefore returns the number of fully-typed argument tokens before the
// cursor, mirroring the logic used by completionSet. This is used by the
// Enter-on-completion accept handler to decide whether a selection is at the
// bottom of the completion chain and should be submitted immediately.
func (c *inlineChat) argsBefore(text string, cursor int) int {
	runes := []rune(text)
	if cursor > len(runes) {
		cursor = len(runes)
	}
	if cursor < 0 {
		cursor = 0
	}
	if !strings.HasPrefix(text, "/") {
		return -1
	}
	before := string(runes[:cursor])
	endsWithSpace := strings.HasSuffix(before, " ")
	fields := strings.Fields(before)
	if len(fields) <= 1 && !endsWithSpace {
		return 0
	}
	argsBefore := len(fields) - 1
	if !endsWithSpace {
		argsBefore--
	}
	return argsBefore
}

// ── Tool box colours ─────────────────────────────────────────────────────────

func toolBoxBg(p theme.ToolStatus) taui.BgFn {
	return func(s string) string { return termkit.FgBgOnly(s, p.FG, p.BG) }
}

// skillToolName is the registry name of the Skill tool. When the executing
// tool is the Skill tool, the TUI renders its box on a purple background and
// shows the activated skill name as the row label instead of raw JSON args.
const skillToolName = "skill"

// skillBoxBg returns the purple background style for the Skill tool's box,
// selected by lifecycle state.
func skillBoxBg(state theme.ToolStatus) taui.BgFn {
	return toolBoxBg(state)
}

// skillStatusLabel extracts a clean, human-readable label from the Skill
// tool's raw JSON arguments (e.g. `{"name":"go-development"}` →
// `go-development`). Falls back to the raw summary if the args don't parse.
func skillStatusLabel(rawArgs string) string {
	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &parsed); err == nil {
		if name := strings.TrimSpace(parsed.Name); name != "" {
			return name
		}
	}
	return rawArgs
}

// ── Controller ──────────────────────────────────────────────────────────────

type inlineCtrl struct{ chat *inlineChat }

func (c *inlineCtrl) HandleInput(data string) bool {
	// The overlay stack tries the active prompt (if any - exclusive, so it
	// consumes everything so normal chat input and the quit guard can't leak
	// through) and then the completions dropdown (soft - only consumes when
	// it has something to show).
	if c.chat.overlays.HandleInput(data) {
		c.chat.engine.RequestRender()
		return true
	}

	switch data {
	case "\x13": // Ctrl+S → steer (idle=submit, busy=inject into current turn)
		c.chat.onSteer(c.chat.input.Value())
		return true

	case "\x1b[71;6u": // Ctrl+Shift+G (Kitty keyboard protocol CSI-u: 'G'=71, shift+ctrl=6) → /copy
		c.chat.handleCopyCommand("")
		return true

	case "\x03": // Ctrl+C
		if c.chat.generating.Load() {
			c.chat.cancelTurn()
			return true
		}
		if c.chat.bashRunning.Load() {
			c.chat.cancelBash()
			return true
		}
		now := time.Now()
		if now.Sub(c.chat.pendingQuit) < quitConfirmWindow {
			c.chat.engine.Terminal.SignalStop()
			go c.chat.engine.Stop()
			return true
		}
		c.chat.pendingQuit = now
		c.chat.mu.Lock()
		if c.chat.notifyQueue != nil {
			c.chat.notifyQueue.Push(notify.Notification{Message: "quit: press Ctrl+C again", Level: notify.LevelInfo, Duration: quitConfirmWindow})
		}
		c.chat.mu.Unlock()
		return true

	case "\x1b": // Escape - cancel generation if running
		if c.chat.generating.Load() {
			c.chat.cancelTurn()
			return true
		}
		if c.chat.bashRunning.Load() {
			c.chat.cancelBash()
			return true
		}
	}

	if c.chat.input.HandleInput(data) {
		c.chat.engine.RequestRender()
		return true
	}
	return false
}

// HandlePaste delivers a bracketed-paste payload following the same
// precedence as HandleInput: the overlay stack gets first refusal (an active
// prompt claims it exclusively; completions doesn't implement PasteHandler
// at all, since it has no text buffer of its own - it reads from
// c.chat.input - so the stack skips it automatically), otherwise the payload
// lands in the main chat input.
func (c *inlineCtrl) HandlePaste(content string) bool {
	if c.chat.overlays.HandlePaste(content) {
		return true
	}
	return c.chat.input.HandlePaste(content)
}

func (c *inlineCtrl) Render(width int) []string { return nil }
func (c *inlineCtrl) Invalidate()               {}
