package layouts

import (
	gt "github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// FormField describes one row in a Form layout.
type FormField struct {
	Label       string
	Description string
	Control     gt.Component
}

// Form is a vertical stack of label+control pairs with Tab navigation
// between controls. The controls must implement gt.Focusable.
type Form struct {
	Fields []FormField
}

// NewForm creates a Form layout.
func NewForm(fields []FormField) *Form {
	return &Form{Fields: fields}
}

// Render builds the form element tree. Controls are rendered via
// MountPersistent at stable indices so focus and keyboard state persist.
func (f *Form) Render(app *gt.App) *gt.Element {
	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithGap(1),
		gt.WithPadding(1),
	)

	for i, field := range f.Fields {
		row := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithGap(0),
		)

		label := gt.New(
			gt.WithText(field.Label),
			gt.WithTextStyle(theme.BodyStyle().Bold()),
		)
		row.AddChild(label)

		control := app.MountPersistent(f, i, func() gt.Component {
			return field.Control
		})
		row.AddChild(control)

		if field.Description != "" {
			desc := gt.New(
				gt.WithText(field.Description),
				gt.WithTextStyle(theme.DimStyle().Italic()),
			)
			row.AddChild(desc)
		}

		root.AddChild(row)
	}

	return root
}
