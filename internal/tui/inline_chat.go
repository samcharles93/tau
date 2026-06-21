package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

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

	runtime tauchat.ChatRuntime
	sub     *eventbus.Subscriber[tauchat.ChatEvent]
	done    chan struct{}

	bold, grey, dim func(string) string

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
	engine *taui.TUI,
	runtime tauchat.ChatRuntime,
	sub *eventbus.Subscriber[tauchat.ChatEvent],
	cfg TUIConfig,
) *inlineChat {
	c := &inlineChat{
		engine:  engine,
		stage:   &taui.Container{},
		runtime: runtime,
		sub:     sub,
		done:    make(chan struct{}),
		bold:    func(s string) string { return termkit.Wrap(s, termkit.Bold) },
		grey:    func(s string) string { return termkit.FgOnly(s, termkit.ColorGrey) },
		dim:     func(s string) string { return termkit.Wrap(s, termkit.Dim, termkit.Italic) },
	}

	c.header = taui.NewStyledText("What can I help you build? (Ctrl+C to quit)", c.grey, nil)
	c.input = taui.NewLineInput("› ")
	c.input.SetStyles(c.bold, nil, c.grey)
	c.input.SetOnSubmit(c.onSubmit)

	c.completions = taui.NewCompletions(c.input, func(ctx taui.CompletionContext) *taui.CompletionSet {
		return inlineCompletions(ctx)
	})

	box := taui.NewBox().Padding(1, 1).ExpandW().Build()
	box.AddChild(c.header)
	box.AddChild(c.stage)
	box.AddChild(taui.NewText(""))
	box.AddChild(c.input)
	box.AddChild(c.completions)

	engine.AddChild(box)
	engine.SetFocus(&inlineCtrl{chat: c})

	go c.spinnerLoop()
	go c.eventLoop()
	return c
}

// ── Spinner ─────────────────────────────────────────────────────────────────

func (c *inlineChat) spinnerLoop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
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

// ── Submit / steer / queue (matches tau's existing patterns) ────────────────

// onSubmit queues the prompt. If no turn is running, it starts one immediately.
// If a turn IS running, the prompt goes into the queue and runs after the
// current turn finishes (non-interrupting).
func (c *inlineChat) onSubmit(prompt string) {
	if strings.TrimSpace(prompt) == "" {
		return
	}
	c.mu.Lock()
	if c.running {
		c.queue = append(c.queue, prompt)
		c.mu.Unlock()
		c.engine.PrintAbove("%s", c.grey("⏳ queued: "+prompt))
		return
	}
	c.running = true
	c.mu.Unlock()
	go c.runTurn(prompt)
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
	_ = c.runtime.Send(tauchat.SteerChatPromptCommand{Prompt: prompt})
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

func (c *inlineChat) close() {
	c.runtime.Close()
	close(c.done)
}

func (c *inlineChat) doTurn(prompt string) {
	c.working.Store(true)
	defer c.working.Store(false)

	c.engine.Update(func() { c.header.SetText(c.grey("Pi is working…")) })
	c.commit(c.bold("› " + prompt))

	err := c.runtime.Send(tauchat.SubmitChatPromptCommand{Prompt: prompt})
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), err.Error())
		c.engine.Update(func() { c.header.SetText(c.grey("What can I help you build? (Ctrl+C to quit)")) })
	}
}

// ── Event handling ──────────────────────────────────────────────────────────

func (c *inlineChat) eventLoop() {
	defer c.runtime.Close()
	for {
		select {
		case <-c.done:
			return
		case ev, ok := <-c.sub.Events():
			if !ok {
				return
			}
			c.handleEvent(ev)
		}
	}
}

func (c *inlineChat) handleEvent(ev tauchat.ChatEvent) {
	switch e := ev.(type) {
	case tauchat.ChatReasoningDeltaEvent:
		c.engine.Update(func() {
			if c.turnReasoning == nil {
				c.turnReasoning = taui.NewParagraph("")
				c.turnReasoning.SetStyle(c.dim)
				c.stage.Clear()
				c.stage.AddChild(c.turnReasoning)
			}
			c.turnReasoning.Append(e.Delta)
		})

	case tauchat.ChatResponseDeltaEvent:
		c.engine.Update(func() {
			if c.turnText == nil {
				c.turnText = taui.NewParagraph("")
				c.stage.Clear()
				c.stage.AddChild(c.turnText)
			}
			c.turnText.Append(e.Delta)
		})

	case tauchat.ChatToolExecutionStartedEvent:
		c.engine.Update(func() {
			tr := taui.NewToolRow(e.ToolName, e.ArgumentsSummary)
			tbox := taui.NewBox().Padding(2, 1).Bg(toolBoxBg(running)).ExpandW().Build()
			tbox.AddChild(tr)
			if c.activeTools == nil {
				c.activeTools = make(map[string]*activeToolBox)
			}
			c.activeTools[e.CallID] = &activeToolBox{row: tr, box: tbox}
			c.stage.AddChild(tbox)
		})

	case tauchat.ChatToolExecutionCompletedEvent:
		c.engine.Update(func() {
			tb, ok := c.activeTools[e.CallID]
			if !ok {
				return
			}
			detail := e.ResultSummary
			if e.IsError {
				detail = "failed"
				tb.row.Fail(detail)
				tb.box.SetBgFn(toolBoxBg(failed))
			} else {
				tb.row.Succeed(detail)
				tb.box.SetBgFn(toolBoxBg(success))
			}
		})
		c.engine.RequestRender()
		time.Sleep(450 * time.Millisecond)

		c.engine.Update(func() {
			tb, ok := c.activeTools[e.CallID]
			if !ok {
				return
			}
			lines := tb.box.Render(c.width())
			c.stage.RemoveChild(tb.box)
			delete(c.activeTools, e.CallID)
			c.commitBlock(lines)
		})

	case tauchat.ChatToolOutputEvent:
		c.engine.PrintAbove("%s", c.grey("  "+e.Chunk))

	case tauchat.ChatResponseCompletedEvent:
		c.engine.Update(func() {
			if c.turnReasoning != nil {
				text := c.turnReasoning.Text()
				if text != "" {
					c.engine.PrintAbove("%s", c.dim(text))
				}
				c.turnReasoning = nil
			}
			if c.turnText != nil {
				text := c.turnText.Text()
				if text != "" {
					c.engine.PrintAbove("%s", text)
				}
				c.turnText = nil
			}
			// Commit any remaining tool boxes, then clear the map.
			for _, tb := range c.activeTools {
				lines := tb.box.Render(c.width())
				c.stage.RemoveChild(tb.box)
				c.commitBlock(lines)
			}
			c.activeTools = nil
			c.stage.Clear()
			c.header.SetText(c.grey("What can I help you build? (Ctrl+C to quit)"))
		})

	case tauchat.ChatRuntimeErrorEvent:
		c.engine.PrintAbove("%s %s", c.grey("✗"), e.Message)
		c.engine.Update(func() {
			c.stage.Clear()
			c.turnText = nil
			c.turnReasoning = nil
			c.activeTools = nil
			c.header.SetText(c.grey("What can I help you build? (Ctrl+C to quit)"))
		})
	}
}

// ── Scrollback helpers ──────────────────────────────────────────────────────

func (c *inlineChat) commit(line string) {
	c.engine.PrintAbove("%s\n", line)
}

func (c *inlineChat) commitBlock(lines []string) {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln + "\x1b[0m"
	}
	c.engine.PrintAbove("%s\n", strings.Join(out, "\n"))
}

func (c *inlineChat) width() int {
	w, _ := c.engine.Terminal.Size()
	if w <= 0 {
		w = 80
	}
	return w
}

// ── Tool box colours (boxdemo palette) ──────────────────────────────────────

type pair struct{ bg, fg termkit.Color }

var (
	running = pair{termkit.Color{252, 214, 187}, termkit.Color{124, 11, 11}}
	success = pair{termkit.Color{192, 234, 172}, termkit.Color{76, 91, 23}}
	failed  = pair{termkit.Color{120, 30, 30}, termkit.Color{248, 209, 207}}
)

func toolBoxBg(p pair) taui.BgFn {
	return func(s string) string { return termkit.FgBgOnly(s, p.fg, p.bg) }
}

// ── Completions ─────────────────────────────────────────────────────────────

func inlineCompletions(ctx taui.CompletionContext) *taui.CompletionSet {
	full := []rune(ctx.Text)
	if ctx.Cursor > len(full) {
		ctx.Cursor = len(full)
	}
	before := string(full[:ctx.Cursor])
	_, last := taui.SplitLastWord(before)
	if last == "" || len([]rune(before)) > len([]rune(last)) {
		return nil
	}
	return &taui.CompletionSet{
		ReplaceStart: len([]rune(before)) - len([]rune(last)),
		ReplaceEnd:   ctx.Cursor,
		Groups: []taui.MatchGroup{{
			Title: "Commands",
			Matches: []taui.Match{
				{Word: "/fix", Description: "— fix the issue"},
				{Word: "/build", Description: "— compile the project"},
				{Word: "/test", Description: "— run the test suite"},
				{Word: "/deploy", Description: "— ship to production"},
				{Word: "/explain", Description: "— explain this code"},
			},
		}},
	}
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
