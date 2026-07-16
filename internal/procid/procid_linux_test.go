//go:build linux

package procid

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestCaptureProcessStartNS_SelfIsNonZero(t *testing.T) {
	ns := CaptureProcessStartNS(os.Getpid())
	if ns == 0 {
		t.Fatal("CaptureProcessStartNS(self) = 0, want a nonzero identity token")
	}
}

func TestCheckPIDIdentity_MatchingIdentityIsAlive(t *testing.T) {
	pid := os.Getpid()
	ns := CaptureProcessStartNS(pid)
	if ns == 0 {
		t.Skip("could not capture own process-start identity")
	}
	if got := CheckPIDIdentity(pid, ns); got != PIDCheckAlive {
		t.Errorf("CheckPIDIdentity(self, matching) = %v, want PIDCheckAlive", got)
	}
}

func TestCheckPIDIdentity_MismatchedIdentityIsDead(t *testing.T) {
	pid := os.Getpid()
	ns := CaptureProcessStartNS(pid)
	if ns == 0 {
		t.Skip("could not capture own process-start identity")
	}
	// A pid that's genuinely alive but whose recorded start identity does
	// not match must be treated as recycled, not alive.
	if got := CheckPIDIdentity(pid, ns+1); got != PIDCheckDead {
		t.Errorf("CheckPIDIdentity(self, mismatched) = %v, want PIDCheckDead", got)
	}
}

func TestCheckPIDIdentity_ZeroWantIsAliveOnAnyRunningPID(t *testing.T) {
	// wantStartNS == 0 means "no identity was ever recorded" - the sweep's
	// documented fallback is "pid existing is the best evidence available".
	if got := CheckPIDIdentity(os.Getpid(), 0); got != PIDCheckAlive {
		t.Errorf("CheckPIDIdentity(self, 0) = %v, want PIDCheckAlive", got)
	}
}

func TestCheckPIDIdentity_DeadPIDIsDead(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run /bin/true: %v", err)
	}
	deadPID := cmd.Process.Pid
	if got := CheckPIDIdentity(deadPID, 0); got != PIDCheckDead {
		t.Errorf("CheckPIDIdentity(dead pid) = %v, want PIDCheckDead", got)
	}
}
