package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// headerIdle is the header text when no turn is running. It is intentionally
// blank — the bottom status line carries the τ tau · model · provider context,
// and the header slot is reused to show the working indicator during a turn.
const headerIdle = ""

type activeToolBox struct {
	row *taui.ToolRow
	box *taui.Box
}

type inlineChat struct {
	engine *taui.TUI

	header      *taui.Text
	stage       *taui.Container
	input       *taui.LineInput
	completions *taui.Completions
	status      *taui.Text

	ctx     context.Context
	runtime tauchat.ChatRuntime
	sub     *eventbus.Subscriber[tauchat.ChatEvent]
	done    chan struct{}

	bold, grey, dim func(string) string

	// Session/model/command state (ported from ChatPanel). Guarded by mu.
	provider          string
	refresh           ModelRefresher
	debug             bool
	notifyQueue       *notify.Queue
	sessionID         string
	modelName         string
	showReasoning     bool
	reasoningEffort   string
	availableModels   []tauchat.ChatModelRef
	registryCommands  []tauchat.CommandRef
	extensionCommands map[string]tauchat.ExtensionCommand
	sessionSummaries  []tauchat.SessionSummary
	lastSubmit        time.Time

	// Per-turn streaming state.
	mu            sync.Mutex
	turnText      *taui.Paragraph
	turnReasoning *taui.Paragraph
	activeTools   map[string]*activeToolBox // parallel tool boxes by CallID
	working       atomic.Bool
	running       bool
	queue         []string
}

func newInlineChat(
	ctx context.Context,
	engine *taui.TUI,
	runtime tauchat.ChatRuntime,
	sub *eventbus.Subscriber[tauchat.ChatEvent],
	cfg TUIConfig,
) *inlineChat {
	c := &inlineChat{
		engine:            engine,
		stage:             &taui.Container{},
		ctx:               ctx,
		runtime:           runtime,
		sub:               sub,
		done:              make(chan struct{}),
		provider:          cfg.Provider,
		refresh:           cfg.RefreshModels,
		debug:             cfg.Debug,
		notifyQueue:       notify.NewQueue(),
		sessionID:         cfg.SessionID,
		modelName:         cfg.ModelName,
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
	c.input = taui.NewLineInput("› ")
	c.input.SetStyles(c.bold, nil, c.grey)
	c.input.SetOnSubmit(c.onSubmit)

	c.completions = taui.NewCompletions(c.input, c.completionSet)
	c.completions.SetOnSelect(func(s string) {
		c.input.SetValueAndCursor(s, len([]rune(s)))
		c.engine.RequestRender()
	})

	box := taui.NewBox().Padding(1, 1).ExpandW().Build()
	box.AddChild(c.header)
	box.AddChild(c.stage)
	box.AddChild(taui.NewText(""))
	box.AddChild(c.input)
	box.AddChild(c.completions)
	box.AddChild(c.status)

	engine.AddChild(box)
	engine.SetFocus(&inlineCtrl{chat: c})

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

func newRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
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
			if !c.working.Load() {
				continue
			}
			c.engine.Update(func() {
				for _, tb := range c.activeTools {
					tb.row.Tick()
				}
			})
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

func (c *inlineChat) computeStatus() string {
	c.mu.Lock()
	model := c.modelName
	prov := c.provider
	var note string
	if c.notifyQueue != nil {
		if cur := c.notifyQueue.Current(); cur != nil {
			note = cur.Message
		}
	}
	c.mu.Unlock()

	parts := []string{"τ tau"}
	if model != "" {
		parts = append(parts, model)
	}
	if prov != "" {
		parts = append(parts, prov)
	}
	s := strings.Join(parts, " · ")
	if note != "" {
		s += " · " + note
	}
	return c.grey(s)
}

// pushNotice queues a transient info notification shown in the status line.
func (c *inlineChat) pushNotice(msg string) {
	c.mu.Lock()
	if c.notifyQueue != nil {
		c.notifyQueue.Push(notify.Notification{Message: msg, Level: notify.LevelInfo, Duration: 5 * time.Second})
	}
	c.mu.Unlock()
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
		c.handleSlashCommand(trimmed)
		return
	}

	c.mu.Lock()
	if c.running {
		c.queue = append(c.queue, trimmed)
		c.mu.Unlock()
		c.engine.PrintAbove("%s", c.grey("⏳ queued: "+trimmed))
		return
	}
	c.running = true
	c.mu.Unlock()
	go c.runTurn(trimmed)
}

// onSteer handles Ctrl+S. When idle it's a normal submit. When busy it sends a
// SteerChatPromptCommand — the coordinator injects it after the next tool call
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
	_ = c.runtime.Send(tauchat.SteerChatPromptCommand{
		SessionID:   c.sid(),
		RequestID:   newRequestID(),
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	})
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
	defer c.working.Store(false)

	c.engine.Update(func() { c.header.SetText(c.grey("τ tau is working…")) })
	c.commit(c.bold("› " + prompt))

	err := c.runtime.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   c.sid(),
		RequestID:   newRequestID(),
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), err.Error())
		c.engine.Update(func() { c.header.SetText(c.grey(headerIdle)) })
	}
}

// ── Scrollback helpers ──────────────────────────────────────────────────────

func (c *inlineChat) commit(line string) {
	c.engine.PrintAbove("%s\n", line)
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

// ── Tool box colours ─────────────────────────────────────────────────────────

type pair struct{ bg, fg termkit.Color }

var (
	running = pair{termkit.Color{252, 214, 187}, termkit.Color{124, 11, 11}}
	success = pair{termkit.Color{192, 234, 172}, termkit.Color{76, 91, 23}}
	failed  = pair{termkit.Color{120, 30, 30}, termkit.Color{248, 209, 207}}
)

func toolBoxBg(p pair) taui.BgFn {
	return func(s string) string { return termkit.FgBgOnly(s, p.fg, p.bg) }
}

// ── Controller ──────────────────────────────────────────────────────────────

type inlineCtrl struct{ chat *inlineChat }

func (c *inlineCtrl) HandleInput(data string) bool {
	if c.chat.completions.HandleInput(data) {
		c.chat.engine.RequestRender()
		return true
	}
	// Ctrl+S → steer (idle=submit, busy=inject into current turn).
	if data == "\x13" {
		c.chat.onSteer(c.chat.input.Value())
		return true
	}
	if c.chat.input.HandleInput(data) {
		c.chat.engine.RequestRender()
		return true
	}
	return false
}

func (c *inlineCtrl) Render(width int) []string { return nil }
func (c *inlineCtrl) Invalidate()               {}
