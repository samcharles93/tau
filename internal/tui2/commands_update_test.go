package tui2

import (
	"context"
	"errors"
	"testing"
)

// /update was an unknown command in the TUI even though `tau update` exists,
// so the only way to check for a release was to leave the session.
func TestUpdateCommandRegistered(t *testing.T) {
	if _, ok := slashIndex["update"]; !ok {
		t.Fatal("/update is not registered in the slash command table")
	}
}

func TestCmdUpdateReportsResult(t *testing.T) {
	m := &model{
		ctx: context.Background(),
		checkUpdate: func(context.Context) (string, error) {
			return "tau v0.31.0 is available - run tau update", nil
		},
	}
	msg, ok := m.cmdUpdate("")().(updateCheckMsg)
	if !ok {
		t.Fatalf("cmdUpdate returned %T, want updateCheckMsg", msg)
	}
	if msg.err != nil || msg.text == "" {
		t.Fatalf("updateCheckMsg = %+v, want text and no error", msg)
	}
}

func TestCmdUpdateReportsError(t *testing.T) {
	m := &model{
		ctx: context.Background(),
		checkUpdate: func(context.Context) (string, error) {
			return "", errors.New("network unreachable")
		},
	}
	msg := m.cmdUpdate("")().(updateCheckMsg)
	if msg.err == nil {
		t.Fatal("updateCheckMsg.err is nil, want the check error surfaced")
	}
}

func TestCmdUpdateWithoutCheckerNotifies(t *testing.T) {
	m := &model{ctx: context.Background()}
	if cmd := m.cmdUpdate(""); cmd == nil {
		t.Fatal("cmdUpdate returned nil with no checker, want a notification")
	}
}
