package termkit

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52Copy(t *testing.T) {
	seq, ok := OSC52Copy("hello clipboard")
	if !ok {
		t.Fatal("OSC52Copy() ok = false, want true")
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello clipboard")) + "\x07"
	if seq != want {
		t.Errorf("OSC52Copy() = %q, want %q", seq, want)
	}
}

func TestOSC52Copy_tooLarge(t *testing.T) {
	seq, ok := OSC52Copy(strings.Repeat("a", OSC52MaxBytes+1))
	if ok {
		t.Error("OSC52Copy() ok = true for oversized payload, want false")
	}
	if seq != "" {
		t.Errorf("OSC52Copy() seq = %q for oversized payload, want empty", seq)
	}
}
