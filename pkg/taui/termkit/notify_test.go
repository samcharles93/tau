package termkit

import (
	"strings"
	"testing"
)

func TestNotify_containsAllProtocols(t *testing.T) {
	got := Notify("tau", "response ready")

	if !strings.Contains(got, "\x1b]99;i=tau-turn:d=0:e=1;") {
		t.Errorf("Notify() missing OSC 99 title chunk: %q", got)
	}
	if !strings.Contains(got, "\x1b]99;i=tau-turn:d=1:e=1:p=body;") {
		t.Errorf("Notify() missing OSC 99 body chunk: %q", got)
	}
	if !strings.Contains(got, "\x1b]777;notify;tau;response ready\x07") {
		t.Errorf("Notify() missing OSC 777 sequence: %q", got)
	}
	if !strings.Contains(got, "\x1b]9;tau: response ready\x07") {
		t.Errorf("Notify() missing OSC 9 sequence: %q", got)
	}
	if !strings.HasSuffix(got, "\a") {
		t.Errorf("Notify() should end with a bell fallback: %q", got)
	}
}

func TestNotify_sanitizesSemicolonsForOSC777(t *testing.T) {
	got := Notify("a;b", "c;d")
	if !strings.Contains(got, "\x1b]777;notify;a,b;c,d\x07") {
		t.Errorf("Notify() did not sanitize semicolons for OSC 777: %q", got)
	}
}

func TestNotify_truncatesLongBody(t *testing.T) {
	long := strings.Repeat("x", notifyBodyMaxRunes+50)
	got := Notify("tau", long)
	if strings.Contains(got, strings.Repeat("x", notifyBodyMaxRunes+1)) {
		t.Error("Notify() did not truncate an over-long body")
	}
}

func TestSanitizeNotifyText(t *testing.T) {
	cases := map[string]string{
		"hello\nworld":   "hello world",
		"a;b;c":          "a,b,c",
		"a\tb":           "a b",
		"a  b   c":       "a b c",
		"\x01hidden\x01": "hidden",
	}
	for in, want := range cases {
		if got := sanitizeNotifyText(in); got != want {
			t.Errorf("sanitizeNotifyText(%q) = %q, want %q", in, got, want)
		}
	}
}
