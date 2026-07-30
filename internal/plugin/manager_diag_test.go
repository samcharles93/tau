package plugin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/plugin/api"
)

// The plugins directory must follow TAU_CONFIG_DIR. Hardcoding
// ~/.config/tau/plugins made a sandboxed run load (and kill) the plugins
// from the user's real config dir.
func TestNewManagerPluginsDirFollowsConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)

	m, err := NewManager(Config{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, "plugins")
	if m.cfg.PluginsDir != want {
		t.Errorf("PluginsDir = %q, want %q", m.cfg.PluginsDir, want)
	}
}

// go-plugin's own logger must never write to stderr: in interactive mode
// stderr is the terminal the TUI is drawing on, so plugin lifecycle noise
// corrupts the display.
func TestNewManagerLogOutputNeverStderr(t *testing.T) {
	m, err := NewManager(Config{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.LogOutput == nil {
		t.Fatal("LogOutput is nil, want a non-nil default sink")
	}
	if m.cfg.LogOutput == os.Stderr {
		t.Error("LogOutput defaults to os.Stderr, which corrupts the TUI")
	}
}

// A plugin built against an older plugin API fails the go-plugin handshake.
// The user must be told, with the remedy, rather than the failure being
// buried in tau.log while raw hclog output scrolls over the TUI.
func TestLoadNotifiesOnVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tau-plugin-stale")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var notes []string
	m, err := NewManager(Config{
		PluginsDir: dir,
		Logger:     slog.New(slog.DiscardHandler),
		Notify: func(level, message string) {
			notes = append(notes, level+": "+message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.spawnPlugin = func(ctx context.Context, path string) (pluginProcess, *api.GRPCClient, error) {
		return nil, nil, errors.New("plugin manager: rpc connect: incompatible API version with plugin. Plugin version: 1, Client versions: [2]")
	}

	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(notes) == 0 {
		t.Fatal("plugin start failure produced no user-visible notification")
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "tau-plugin-stale") {
		t.Errorf("notification does not name the plugin: %q", joined)
	}
	if !strings.Contains(strings.ToLower(joined), "rebuild") {
		t.Errorf("notification does not tell the user to rebuild: %q", joined)
	}
}

// startPluginHint turns an opaque handshake error into an actionable one.
func TestStartFailureHint(t *testing.T) {
	mismatch := errors.New("incompatible API version with plugin. Plugin version: 1, Client versions: [2]")
	got := startFailureHint("tau-plugin-mcp", mismatch)
	if !strings.Contains(got, "tau-plugin-mcp") || !strings.Contains(strings.ToLower(got), "rebuild") {
		t.Errorf("hint = %q, want it to name the plugin and say rebuild", got)
	}

	other := errors.New("permission denied")
	if got := startFailureHint("tau-plugin-mcp", other); !strings.Contains(got, "permission denied") {
		t.Errorf("hint = %q, want the underlying error preserved", got)
	}
}
