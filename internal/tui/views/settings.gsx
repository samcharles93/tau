package views

import (
	tui "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// SettingsView is the fullscreen alternate screen settings dashboard.
type SettingsView struct {
	show               *tui.State[bool]
	provider           string
	modelName          *tui.State[string]
	availableModels    *tui.State[[]tauchat.ChatModelRef]
	selectedModelIndex *tui.State[int]
	showReasoning      *tui.State[bool]
	sessionID          func() string
	onSelectModel      func(string)
}

// NewSettingsView creates a new settings view.
func NewSettingsView(
	show *tui.State[bool],
	provider string,
	modelName *tui.State[string],
	availableModels *tui.State[[]tauchat.ChatModelRef],
	selectedModelIndex *tui.State[int],
	showReasoning *tui.State[bool],
	sessionID func() string,
	onSelectModel func(string),
) *SettingsView {
	return &SettingsView{
		show:               show,
		provider:           provider,
		modelName:          modelName,
		availableModels:    availableModels,
		selectedModelIndex: selectedModelIndex,
		showReasoning:      showReasoning,
		sessionID:          sessionID,
		onSelectModel:      onSelectModel,
	}
}

// BindApp wires all reactive state to the running app.
func (v *SettingsView) BindApp(app *tui.App) {
	v.show.BindApp(app)
	v.modelName.BindApp(app)
	v.availableModels.BindApp(app)
	v.selectedModelIndex.BindApp(app)
	v.showReasoning.BindApp(app)
}

// KeyMap processes alternate screen key navigation for settings.
func (v *SettingsView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.On(tui.KeyEscape, func(ke tui.KeyEvent) {
			v.show.Set(false)
		}),
		tui.On(tui.KeyUp, func(ke tui.KeyEvent) {
			models := v.availableModels.Get()
			if len(models) == 0 {
				return
			}
			idx := v.selectedModelIndex.Get() - 1
			if idx < 0 {
				idx = len(models) - 1
			}
			v.selectedModelIndex.Set(idx)
		}),
		tui.On(tui.KeyDown, func(ke tui.KeyEvent) {
			models := v.availableModels.Get()
			if len(models) == 0 {
				return
			}
			idx := v.selectedModelIndex.Get() + 1
			if idx >= len(models) {
				idx = 0
			}
			v.selectedModelIndex.Set(idx)
		}),
		tui.On(tui.KeyEnter, func(ke tui.KeyEvent) {
			models := v.availableModels.Get()
			if len(models) > 0 {
				idx := v.selectedModelIndex.Get()
				if idx >= 0 && idx < len(models) && models[idx].Ready {
					if v.onSelectModel != nil {
						v.onSelectModel(models[idx].ID)
					}
				}
			}
			v.showReasoning.Set(!v.showReasoning.Get())
		}),
	}
}

func (v *SettingsView) modelStyle(i int, model tauchat.ChatModelRef) tui.Style {
	currentModel := v.modelName.Get()
	selIdx := v.selectedModelIndex.Get()
	style := theme.DimStyle()
	if model.ID == currentModel {
		style = theme.BrandStyle()
	} else if i == selIdx && model.Ready {
		style = theme.BodyStyle()
	}
	if !model.Ready {
		style = theme.DimStyle().Italic()
	}
	return style
}

func (v *SettingsView) modelLabel(i int, model tauchat.ChatModelRef) string {
	currentModel := v.modelName.Get()
	selIdx := v.selectedModelIndex.Get()
	prefix := "  "
	if model.ID == currentModel {
		prefix = "✓ "
	} else if i == selIdx && model.Ready {
		prefix = "› "
	}
	label := prefix + model.ID
	if !model.Ready {
		label += " (not ready)"
	}
	return label
}

templ (v *SettingsView) Render() {
	<modal open={v.show} class="justify-center items-center" backdrop="dim" trapFocus={false}>
		<div class="flex-col w-56 border-rounded p-1 gap-1" borderStyle={theme.BrandStyle()}>
			<span textStyle={theme.BrandStyle()}>{"Settings"}</span>
			<span textStyle={theme.BodyStyle()} class="truncate">{"Provider: " + v.provider}</span>
			<span textStyle={theme.BodyStyle()} class="truncate">{"Model: " + v.modelName.Get()}</span>

			<div class="flex-col max-h-12 overflow-y-scroll gap-0">
				for i, model := range v.availableModels.Get() {
					<span textStyle={v.modelStyle(i, model)} class="truncate">
						{v.modelLabel(i, model)}
					</span>
				}
			</div>

			if v.showReasoning.Get() {
				<span textStyle={theme.BodyStyle()} class="truncate">
					{"Reasoning: on  (↑↓ models  Enter switch/toggle  Ctrl+R)"}
				</span>
			} else {
				<span textStyle={theme.BodyStyle()} class="truncate">
					{"Reasoning: off  (↑↓ models  Enter switch/toggle  Ctrl+R)"}
				</span>
			}

			if sid := v.sessionID(); sid != "" {
				<span textStyle={theme.DimStyle()} class="truncate">{"Session: " + sid}</span>
			}
			<span textStyle={theme.DimStyle().Italic()}>
				{"↑↓: navigate models  Enter: switch/toggle  Esc: close"}
			</span>
		</div>
	</modal>
}
