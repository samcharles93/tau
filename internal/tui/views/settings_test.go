package views

import (
	"testing"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestSettingsView_SelectsModel(t *testing.T) {
	var selectedID string
	show := gt.NewState(true)
	modelName := gt.NewState("model-a")
	availableModels := gt.NewState([]tauchat.ChatModelRef{
		{ID: "model-a", Ready: true},
		{ID: "model-b", Ready: true},
		{ID: "model-c", Ready: false},
	})
	selectedIndex := gt.NewState(0)
	showReasoning := gt.NewState(false)

	v := NewSettingsView(
		show,
		"test-provider",
		modelName,
		availableModels,
		selectedIndex,
		showReasoning,
		func() string { return "session-1" },
		func(id string) { selectedID = id },
	)

	// Select model-b (index 1).
	selectedIndex.Set(1)
	v.handleListSelect(v.buildItems()[1])

	if selectedID != "model-b" {
		t.Fatalf("selected model = %q, want model-b", selectedID)
	}
}

func TestSettingsView_SkipsDisabledModel(t *testing.T) {
	var selectedID string
	show := gt.NewState(true)
	modelName := gt.NewState("model-a")
	availableModels := gt.NewState([]tauchat.ChatModelRef{
		{ID: "model-a", Ready: true},
		{ID: "model-c", Ready: false},
	})
	selectedIndex := gt.NewState(0)
	showReasoning := gt.NewState(false)

	v := NewSettingsView(
		show,
		"test-provider",
		modelName,
		availableModels,
		selectedIndex,
		showReasoning,
		func() string { return "" },
		func(id string) { selectedID = id },
	)

	items := v.buildItems()
	if !items[1].Disabled {
		t.Fatal("not-ready model should be disabled")
	}

	// Selecting the disabled item still calls onSelectModel because
	// the List skips disabled items during navigation; the callback
	// receives whatever the list decided to activate.
	v.handleListSelect(items[1])
	if selectedID != "model-c" {
		t.Fatalf("callback id = %q, want model-c", selectedID)
	}
}

func TestSettingsView_KeyMapEscCloses(t *testing.T) {
	show := gt.NewState(true)
	v := NewSettingsView(
		show,
		"test-provider",
		gt.NewState("model-a"),
		gt.NewState([]tauchat.ChatModelRef{}),
		gt.NewState(0),
		gt.NewState(false),
		func() string { return "" },
		nil,
	)

	for _, b := range v.KeyMap() {
		if b.Pattern.Key == gt.KeyEscape {
			b.Handler(gt.KeyEvent{Key: gt.KeyEscape})
		}
	}

	if show.Get() {
		t.Error("settings should be closed after Esc")
	}
}
