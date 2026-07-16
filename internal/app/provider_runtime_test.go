package app

import (
	"context"
	"strings"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestProviderRuntimeReloadToEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
	for _, entry := range providers.Catalog() {
		for _, name := range entry.EnvVars {
			t.Setenv(name, "")
		}
	}
	state, err := providers.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range providers.Catalog() {
		if entry.Auth == providers.AuthNone {
			state.Disable(entry.ID)
		}
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	initial := []tauconfig.ProviderConfig{{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
	}}
	pr := newProviderRuntime(newRuntimeForProviders(initial, false), initial, false)

	if err := pr.reload(context.Background()); err != nil {
		t.Fatalf("reload() error = %v", err)
	}
	rt, configured := pr.snapshot()
	if rt == nil {
		t.Fatal("reload() installed a nil runtime")
	}
	if len(configured) != 0 {
		t.Fatalf("reload() providers = %v, want empty", configured)
	}

	streamer := buildDynamicStreamer(pr)
	_, err = streamer.StreamChatCompletionFull(
		context.Background(),
		tauchat.ChatSessionState{
			Provider: tauconfig.ProviderConfig{Name: "deepseek"},
			Model:    tauchat.ChatModelRef{ID: "deepseek-chat"},
		},
		"",
		nil,
		tauchat.StreamCallbacks{},
	)
	if err == nil {
		t.Fatal("expected an error after reloading to an empty provider set")
	}
	if !strings.Contains(err.Error(), "no provider selected") || !strings.Contains(err.Error(), "/provider") {
		t.Fatalf("expected friendly provider guidance, got %v", err)
	}
}
