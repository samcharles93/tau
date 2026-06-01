package views

import (
	"fmt"
	"strings"
	"time"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// SessionInfoView shows formatted session metadata in a modal.
type SessionInfoView struct {
	show    *gt.State[bool]
	summary *gt.State[tauchat.SessionSummary]
	modal   *gt.Modal
}

// NewSessionInfoView creates the session info modal.
func NewSessionInfoView(show *gt.State[bool], summary *gt.State[tauchat.SessionSummary]) *SessionInfoView {
	return &SessionInfoView{
		show:    show,
		summary: summary,
		modal: gt.NewModal(
			gt.WithModalOpen(show),
			gt.WithModalBackdrop("dim"),
			gt.WithModalTrapFocus(true),
		),
	}
}

// BindApp wires reactive state.
func (v *SessionInfoView) BindApp(app *gt.App) {
	v.show.BindApp(app)
	v.summary.BindApp(app)
	v.modal.BindApp(app)
}

// Render builds the session info modal.
func (v *SessionInfoView) Render(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(v, 1, func() gt.Component {
		return v.modal
	})

	content := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(52),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BrandStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
	)

	s := v.summary.Get()

	// Title.
	content.AddChild(gt.New(
		gt.WithText("Session Info"),
		gt.WithTextStyle(theme.BrandStyle()),
	))

	// ID.
	content.AddChild(renderInfoField("ID", s.ID))

	// Dates.
	content.AddChild(renderInfoField("Created", formatSessionTime(s.CreatedAt)))
	content.AddChild(renderInfoField("Updated", formatSessionTime(s.UpdatedAt)))

	// Model & Provider.
	content.AddChild(renderInfoField("Model", s.ModelID))
	content.AddChild(renderInfoField("Provider", s.Provider))

	// Messages.
	content.AddChild(gt.New(gt.WithText("")))
	content.AddChild(gt.New(
		gt.WithText("Messages"),
		gt.WithTextStyle(theme.BodyStyle().Bold()),
	))
	content.AddChild(renderInfoField("Count", fmt.Sprintf("%d", s.MessageCount)))

	// Tokens.
	content.AddChild(gt.New(gt.WithText("")))
	content.AddChild(gt.New(
		gt.WithText("Tokens"),
		gt.WithTextStyle(theme.BodyStyle().Bold()),
	))
	content.AddChild(renderInfoField("Input", formatTokens(s.InputTokens)))
	content.AddChild(renderInfoField("Output", formatTokens(s.OutputTokens)))
	content.AddChild(renderInfoField("Total", formatTokens(s.TotalTokens)))

	// Cost.
	content.AddChild(gt.New(gt.WithText("")))
	content.AddChild(gt.New(
		gt.WithText("Cost"),
		gt.WithTextStyle(theme.BodyStyle().Bold()),
	))
	content.AddChild(renderInfoField("Total", "$"+formatCost(s.Cost)))

	// Duration.
	if s.DurationMs > 0 {
		dur := time.Duration(s.DurationMs) * time.Millisecond
		content.AddChild(renderInfoField("Duration", dur.Round(time.Second).String()))
	}

	// System prompt (truncated).
	if strings.TrimSpace(s.SystemPrompt) != "" {
		content.AddChild(gt.New(gt.WithText("")))
		content.AddChild(gt.New(
			gt.WithText("System Prompt"),
			gt.WithTextStyle(theme.BodyStyle().Bold()),
		))
		prompt := s.SystemPrompt
		if len(prompt) > 120 {
			prompt = prompt[:117] + "…"
		}
		content.AddChild(renderInfoField("", prompt))
	}

	content.AddChild(gt.New(
		gt.WithText("Esc to close"),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))

	modalEl.AddChild(content)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(modalEl)
	return wrapper
}

// KeyMap for the info view — Esc closes.
func (v *SessionInfoView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			v.show.Set(false)
		}),
	}
}

func renderInfoField(label, value string) *gt.Element {
	row := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithGap(2),
	)

	if label != "" {
		row.AddChild(gt.New(
			gt.WithText(label+":"),
			gt.WithWidth(10),
			gt.WithTextStyle(theme.DimStyle()),
		))
	}

	row.AddChild(gt.New(
		gt.WithText(value),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithTruncate(true),
	))
	return row
}

func formatSessionTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
