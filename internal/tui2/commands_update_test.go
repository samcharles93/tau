package tui2

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// /update was an unknown command in the TUI even though `tau update` exists,
// so the only way to update was to leave the session.
func TestUpdateCommandRegistered(t *testing.T) {
	if _, ok := slashIndex["update"]; !ok {
		t.Fatal("/update is not registered in the slash command table")
	}
}

// Bare /update installs; only `/update check` stops at reporting.
func TestCmdUpdateInstallsByDefault(t *testing.T) {
	var gotInstall bool
	m := &model{
		ctx: context.Background(),
		updateFn: func(_ context.Context, install bool) (string, bool, error) {
			gotInstall = install
			return "updated to v0.31.0", true, nil
		},
	}
	msg, ok := m.cmdUpdate("")().(updateCheckMsg)
	if !ok {
		t.Fatalf("cmdUpdate returned %T, want updateCheckMsg", msg)
	}
	if !gotInstall {
		t.Error("bare /update ran a check-only pass, want an install")
	}
	if !msg.restart {
		t.Error("successful install did not ask for a restart")
	}
}

func TestCmdUpdateCheckDoesNotInstall(t *testing.T) {
	var gotInstall bool
	m := &model{
		ctx: context.Background(),
		updateFn: func(_ context.Context, install bool) (string, bool, error) {
			gotInstall = install
			return "tau v0.31.0 is available", false, nil
		},
	}
	msg := m.cmdUpdate("check")().(updateCheckMsg)
	if gotInstall {
		t.Error("/update check installed, want check only")
	}
	if msg.restart {
		t.Error("/update check asked for a restart")
	}
}

func TestCmdUpdateReportsError(t *testing.T) {
	m := &model{
		ctx: context.Background(),
		updateFn: func(context.Context, bool) (string, bool, error) {
			return "", false, errors.New("network unreachable")
		},
	}
	if msg := m.cmdUpdate("")().(updateCheckMsg); msg.err == nil {
		t.Fatal("updateCheckMsg.err is nil, want the failure surfaced")
	}
}

func TestCmdUpdateWithoutFuncNotifies(t *testing.T) {
	m := &model{ctx: context.Background()}
	if cmd := m.cmdUpdate(""); cmd == nil {
		t.Fatal("cmdUpdate returned nil with no update func, want a notification")
	}
}

// A completed install must quit the TUI so the app layer can re-exec the
// replaced binary; leaving the old process running would keep the old code.
func TestUpdateRestartQuitsTUI(t *testing.T) {
	m := &model{ctx: context.Background()}
	_, cmd := m.Update(updateCheckMsg{text: "updated to v0.31.0", restart: true})
	if cmd == nil {
		t.Fatal("restart-pending update produced no command, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("command produced %T, want tea.QuitMsg", cmd())
	}
}
