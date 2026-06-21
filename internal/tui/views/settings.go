package views

import (
	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
)

// SettingsView is the fullscreen alternate-screen settings dashboard.
// It shows the current provider/model, a reasoning toggle, and a
// selectable model list.
type SettingsView struct {
	show               *gt.State[bool]
	provider           string
	modelName          *gt.State[string]
	availableModels    *gt.State[[]tauchat.ChatModelRef]
	selectedModelIndex *gt.State[int]
	showReasoning      *gt.State[bool]
	sessionID          func() string
	onSelectModel      func(string)
	list               *components.List
	items              *gt.State[[]components.ListItem]
}

// NewSettingsView creates a new settings view.
func NewSettingsView(
	show *gt.State[bool],
	provider string,
	modelName *gt.State[string],
	availableModels *gt.State[[]tauchat.ChatModelRef],
	selectedModelIndex *gt.State[int],
	showReasoning *gt.State[bool],
	sessionID func() string,
	onSelectModel func(string),
) *SettingsView {
	items := gt.NewState([]components.ListItem{})
	v := &SettingsView{
		show:               show,
		provider:           provider,
		modelName:          modelName,
		availableModels:    availableModels,
		selectedModelIndex: selectedModelIndex,
		showReasoning:      showReasoning,
		sessionID:          sessionID,
		onSelectModel:      onSelectModel,
		items:              items,
	}
	v.list = components.NewList(items, selectedModelIndex, v.handleListSelect)
	return v
}

// BindApp wires all reactive state to the running app.
func (v *SettingsView) BindApp(app *gt.App) {
	v.show.BindApp(app)
	v.modelName.BindApp(app)
	v.availableModels.BindApp(app)
	v.selectedModelIndex.BindApp(app)
	v.showReasoning.BindApp(app)
	v.items.BindApp(app)
	v.list.BindApp(app)
	// Focus the list when the view opens so arrow keys navigate models.
	v.show.Bind(func(open bool) {
		if open {
			v.list.Focus()
		}
	})
}

// KeyMap processes alternate-screen key navigation for settings.
func (v *SettingsView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			v.show.Set(false)
		}),
	}
}

// Render builds the full-screen settings view.
func (v *SettingsView) Render(app *gt.App) *gt.Element {
	v.items.Set(v.buildItems())

	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
		gt.WithPadding(1),
		gt.WithGap(1),
	)

	header := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexShrink(0),
		gt.WithGap(0),
	)
	header.AddChild(gt.New(
		gt.WithText("Settings"),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	header.AddChild(gt.New(
		gt.WithText("Provider: "+v.provider),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithTruncate(true),
	))
	header.AddChild(gt.New(
		gt.WithText("Model: "+v.modelName.Get()),
		gt.WithTextStyle(theme.BodyStyle()),
		gt.WithTruncate(true),
	))
	root.AddChild(header)

	if reasoning := v.showReasoning.Get(); reasoning {
		root.AddChild(gt.New(
			gt.WithText("Reasoning: on"),
			gt.WithTextStyle(theme.BodyStyle()),
		))
	} else {
		root.AddChild(gt.New(
			gt.WithText("Reasoning: off"),
			gt.WithTextStyle(theme.BodyStyle()),
		))
	}

	if sid := v.sessionID(); sid != "" {
		root.AddChild(gt.New(
			gt.WithText("Session: "+sid),
			gt.WithTextStyle(theme.DimStyle()),
			gt.WithTruncate(true),
		))
	}

	listContainer := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexGrow(1),
		gt.WithBorder(gt.BorderSingle),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithPadding(1),
	)
	listEl := app.MountPersistent(v, 0, func() gt.Component {
		return v.list
	})
	listContainer.AddChild(listEl)
	root.AddChild(listContainer)

	root.AddChild(gt.New(
		gt.WithText("↑↓: navigate models  ·  Enter: switch model  ·  Esc: close  ·  Ctrl+R: toggle reasoning"),
		gt.WithTextStyle(theme.DescriptionStyle()),
		gt.WithFlexShrink(0),
		gt.WithTruncate(true),
	))

	return root
}

func (v *SettingsView) buildItems() []components.ListItem {
	models := v.availableModels.Get()
	current := v.modelName.Get()
	items := make([]components.ListItem, 0, len(models))
	for _, model := range models {
		label := model.ID
		if model.ID == current {
			label = "✓ " + model.ID
		}
		description := model.URL
		items = append(items, components.ListItem{
			ID:          model.ID,
			Label:       label,
			Description: description,
		})
	}
	return items
}

func (v *SettingsView) handleListSelect(item components.ListItem) {
	if v.onSelectModel != nil {
		v.onSelectModel(item.ID)
	}
}

var _ gt.KeyListener = (*SettingsView)(nil)
