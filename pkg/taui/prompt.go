package taui

import (
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// PromptKind distinguishes a yes/no confirmation from a free-text question.
type PromptKind int

const (
	PromptConfirm PromptKind = iota
	PromptQuestion
)

// Prompt is a small modal-style component for interactive confirm/question
// dialogs — e.g. those raised by plugins and tools via the host's
// Confirm/Input API. The caller is responsible for giving it exclusive input
// focus while it is visible and for removing it from the component tree once
// resolved (via OnConfirm/OnAnswer/OnCancel).
type Prompt struct {
	kind    PromptKind
	title   string
	message string

	input      *LineInput // question mode
	confirmYes bool       // confirm mode: which option is highlighted

	onConfirm func(confirmed bool)
	onAnswer  func(value string)
	onCancel  func()

	titleFn, msgFn func(string) string
}

// NewConfirmPrompt creates a yes/no confirmation prompt. Yes is highlighted
// by default; arrow keys or Tab switch the highlighted option.
func NewConfirmPrompt(title, message string) *Prompt {
	return &Prompt{kind: PromptConfirm, title: title, message: message, confirmYes: true}
}

// NewQuestionPrompt creates a free-text input prompt.
func NewQuestionPrompt(title, placeholder string) *Prompt {
	li := NewLineInput("› ")
	li.SetPlaceholder(placeholder)
	p := &Prompt{kind: PromptQuestion, title: title, input: li}
	li.SetOnSubmit(func(v string) {
		if p.onAnswer != nil {
			p.onAnswer(v)
		}
	})
	return p
}

// SetOnConfirm registers the callback fired when a confirm prompt is answered.
func (p *Prompt) SetOnConfirm(fn func(confirmed bool)) { p.onConfirm = fn }

// SetOnAnswer registers the callback fired when a question prompt is submitted.
func (p *Prompt) SetOnAnswer(fn func(value string)) { p.onAnswer = fn }

// SetOnCancel registers the callback fired when the prompt is dismissed
// without an answer (Esc or Ctrl+C).
func (p *Prompt) SetOnCancel(fn func()) { p.onCancel = fn }

// SetStyles sets colour callbacks for the title and message/hint text.
func (p *Prompt) SetStyles(titleFn, msgFn func(string) string) {
	p.titleFn, p.msgFn = titleFn, msgFn
	if p.input != nil {
		p.input.SetStyles(nil, nil, msgFn)
	}
}

// Invalidate clears any cached render state.
func (p *Prompt) Invalidate() {
	if p.input != nil {
		p.input.Invalidate()
	}
}

// Render draws the title, wrapped message, and the confirm options or
// question input line.
func (p *Prompt) Render(width int) []string {
	title := p.title
	if p.titleFn != nil {
		title = p.titleFn(title)
	}
	lines := []string{"● " + title}

	if p.message != "" {
		for _, ln := range wrapWords(p.message, max(1, width-2)) {
			if p.msgFn != nil {
				ln = p.msgFn(ln)
			}
			lines = append(lines, "  "+ln)
		}
	}

	switch p.kind {
	case PromptConfirm:
		yes, no := "Yes", "No"
		if p.confirmYes {
			yes = termkit.Wrap(yes, termkit.Bold, termkit.Underline)
		} else {
			no = termkit.Wrap(no, termkit.Bold, termkit.Underline)
		}
		hint := "  " + yes + "   " + no
		if p.msgFn != nil {
			hint += p.msgFn("  (y/n · tab to switch · enter to confirm · esc to cancel)")
		}
		lines = append(lines, hint)
	case PromptQuestion:
		lines = append(lines, p.input.Render(width)...)
	}
	return lines
}

// HandlePaste delivers a bracketed-paste payload to the question-mode input.
// Confirm prompts have nowhere to put pasted text, so it's dropped.
func (p *Prompt) HandlePaste(content string) bool {
	if p.kind == PromptQuestion {
		return p.input.HandlePaste(content)
	}
	return false
}

// HandleInput applies one key sequence. Callers should treat the input as
// consumed while a Prompt is active regardless of the return value.
func (p *Prompt) HandleInput(data string) bool {
	switch data {
	case "\x1b", "\x03": // Esc, Ctrl+C — cancel
		if p.onCancel != nil {
			p.onCancel()
		}
		return true
	}

	switch p.kind {
	case PromptConfirm:
		switch data {
		case "y", "Y":
			if p.onConfirm != nil {
				p.onConfirm(true)
			}
			return true
		case "n", "N":
			if p.onConfirm != nil {
				p.onConfirm(false)
			}
			return true
		case "\t", "\x1b[C", "\x1bOC", "\x1b[D", "\x1bOD":
			p.confirmYes = !p.confirmYes
			return true
		case "\r", "\n":
			if p.onConfirm != nil {
				p.onConfirm(p.confirmYes)
			}
			return true
		}
		return false
	case PromptQuestion:
		return p.input.HandleInput(data)
	}
	return false
}
