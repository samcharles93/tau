package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// promptKind distinguishes a yes/no confirmation from a free-text question.
type promptKind int

const (
	promptConfirm promptKind = iota
	promptQuestion
)

// formPrompt is tui2's one reusable modal-input widget: a title, message,
// and either a Yes/No toggle (confirm) or a *textField (question). It's
// deliberately not tied to the agent-question protocol - resolving it just
// invokes a plain Go callback - so the same widget backs both:
//   - agent-originated prompts (enqueuePrompt adapts an incoming
//     InteractivePromptRequestedEvent into a formPrompt whose callbacks send
//     a RespondInteractivePromptCommand back to the runtime), and
//   - local UI flows with no chat round-trip at all (see presentLocalPrompt,
//     used by providerLogin's GitHub Copilot Enterprise-domain prompt).
type formPrompt struct {
	kind    promptKind
	title   string
	message string

	// requestID identifies an agent-originated prompt (mirrors the
	// InteractivePromptRequestedEvent it was built from) - empty for
	// locally-presented prompts, which have no chat round-trip to
	// correlate. Kept for identifying/logging which prompt is active or
	// queued; resolution itself goes through onAnswer/onConfirm/onCancel,
	// not this field.
	requestID string

	field      *textField // question kind only
	confirmYes bool       // confirm kind only - which option is highlighted

	onAnswer  func(value string) tea.Cmd
	onConfirm func(confirmed bool) tea.Cmd
	onCancel  func() tea.Cmd
}

// enqueuePrompt adapts an agent-originated InteractivePromptRequestedEvent
// into a formPrompt and presents it. This is the agent-question half of the
// shared prompt widget.
func (m *model) enqueuePrompt(e tauchat.InteractivePromptRequestedEvent) tea.Cmd {
	kind := promptQuestion
	if e.Kind == tauchat.InteractivePromptConfirm {
		kind = promptConfirm
	}
	respond := func(cmd tauchat.RespondInteractivePromptCommand) tea.Cmd {
		cmd.RequestID = e.RequestID
		cmd.RespondedAt = time.Now().UTC()
		return sendCommand(m.runtime, cmd)
	}
	p := &formPrompt{
		kind:       kind,
		title:      e.Title,
		message:    e.Message,
		requestID:  e.RequestID,
		confirmYes: true,
		onAnswer: func(value string) tea.Cmd {
			return respond(tauchat.RespondInteractivePromptCommand{Response: value})
		},
		onConfirm: func(confirmed bool) tea.Cmd {
			return respond(tauchat.RespondInteractivePromptCommand{Confirmed: confirmed, Canceled: !confirmed})
		},
		onCancel: func() tea.Cmd {
			return respond(tauchat.RespondInteractivePromptCommand{Canceled: true})
		},
	}
	if kind == promptQuestion {
		p.field = newTextField("")
	}
	return m.presentPrompt(p)
}

// presentLocalPrompt shows a free-text question prompt driven by local UI
// code rather than the agent - e.g. providerLogin's Enterprise-domain
// prompt. Resolving it never sends a chat command; it just invokes the
// given callback, so local flows can kick off their own tea.Cmd (starting
// an OAuth login, say) from the answer.
func (m *model) presentLocalPrompt(title, message, placeholder string, onAnswer func(string) tea.Cmd, onCancel func() tea.Cmd) tea.Cmd {
	p := &formPrompt{
		kind:     promptQuestion,
		title:    title,
		message:  message,
		field:    newTextField(placeholder),
		onAnswer: onAnswer,
		onCancel: onCancel,
	}
	return m.presentPrompt(p)
}

// presentPrompt shows p immediately if no prompt is active, or queues it
// behind the current one. Shared by enqueuePrompt and presentLocalPrompt.
func (m *model) presentPrompt(p *formPrompt) tea.Cmd {
	// A prompt represents a blocking round-trip and must win over a UI
	// affordance the user can freely re-open - close any open context menu
	// so it can't linger swallowing keys the prompt needs.
	m.contextMenu = nil
	if m.activePrompt != nil {
		m.promptQueue = append(m.promptQueue, p)
		return m.setNotification("prompt queued")
	}
	m.activePrompt = p
	m.clearInput()
	return nil
}

// presentNextQueuedPrompt pops the next queued prompt (if any) into
// activePrompt.
func (m *model) presentNextQueuedPrompt() {
	m.activePrompt = nil
	m.clearInput()
	if len(m.promptQueue) == 0 {
		return
	}
	m.activePrompt = m.promptQueue[0]
	m.promptQueue = m.promptQueue[1:]
}

func (m *model) handlePromptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		return m.resolvePromptCancel()
	case "enter":
		return m.resolvePrompt()
	case "y", "Y":
		if p.kind == promptConfirm {
			return m.resolvePromptConfirm(true)
		}
		// msg.String() is used rather than msg.Key().Text since the switch
		// above already guarantees it's exactly "y"/"Y" here.
		p.field.Insert(msg.String())
	case "n", "N":
		if p.kind == promptConfirm {
			return m.resolvePromptConfirm(false)
		}
		p.field.Insert(msg.String())
	case "tab":
		if p.kind == promptConfirm {
			p.confirmYes = !p.confirmYes
		}
	case "left":
		if p.kind == promptConfirm {
			p.confirmYes = !p.confirmYes
		} else {
			p.field.MoveCursor(-1)
		}
	case "right":
		if p.kind == promptConfirm {
			p.confirmYes = !p.confirmYes
		} else {
			p.field.MoveCursor(1)
		}
	case "home":
		if p.kind == promptQuestion {
			p.field.Home()
		}
	case "end":
		if p.kind == promptQuestion {
			p.field.End()
		}
	case "delete":
		if p.kind == promptQuestion {
			p.field.Delete()
		}
	case "backspace":
		if p.kind == promptQuestion {
			p.field.Backspace()
		}
	default:
		if p.kind == promptQuestion {
			if text := msg.Key().Text; text != "" {
				if r, _ := utf8.DecodeRuneInString(text); r >= 32 && r != utf8.RuneError {
					p.field.Insert(text)
				}
			}
		}
	}
	return nil
}

func (m *model) resolvePrompt() tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	if p.kind == promptConfirm {
		return m.resolvePromptConfirm(p.confirmYes)
	}
	var answer string
	if p.field != nil {
		answer = p.field.Value()
	}
	m.presentNextQueuedPrompt()
	if p.onAnswer != nil {
		return p.onAnswer(answer)
	}
	return nil
}

func (m *model) resolvePromptConfirm(confirmed bool) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	m.presentNextQueuedPrompt()
	if p.onConfirm != nil {
		return p.onConfirm(confirmed)
	}
	return nil
}

func (m *model) resolvePromptCancel() tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	m.presentNextQueuedPrompt()
	if p.onCancel != nil {
		return p.onCancel()
	}
	return nil
}

// renderPrompt draws the prompt as a plain inline block - a styled title,
// a word-wrapped message, then either a Yes/No toggle (confirm) or the live
// text field (question) - matching the chat composer's unboxed aesthetic
// rather than a bordered modal, per Sam's steer away from the earlier
// fixed-width ASCII box.
func renderPrompt(p *formPrompt, width int) string {
	width = max(width, 20)
	msgWidth := max(width-2, 1)

	var sb strings.Builder
	sb.WriteString(promptTitleStyle.Render(p.title))
	if p.message != "" {
		sb.WriteString("\n")
		wrapped := lipgloss.NewStyle().Width(msgWidth).Render(p.message)
		for line := range strings.SplitSeq(wrapped, "\n") {
			sb.WriteString(promptTextStyle.Render("  " + line))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	if p.kind == promptConfirm {
		yes, no := "Yes", "No"
		if p.confirmYes {
			yes = promptHighlightStyle.Render(yes)
		} else {
			no = promptHighlightStyle.Render(no)
		}
		sb.WriteString("  ")
		sb.WriteString(yes)
		sb.WriteString("   ")
		sb.WriteString(no)
		sb.WriteString("\n\n")
		sb.WriteString(promptHintStyle.Render("  y/n · tab to switch · enter to confirm · esc to cancel"))
	} else {
		fieldPrefix := inputPromptStyle.Render("> ")
		fieldWidth := max(msgWidth-lipgloss.Width(fieldPrefix), 1)
		sb.WriteString("  ")
		sb.WriteString(fieldPrefix)
		sb.WriteString(p.field.Render(fieldWidth))
		sb.WriteString("\n\n")
		sb.WriteString(promptHintStyle.Render("  enter to continue · esc to cancel"))
	}
	return sb.String()
}
