package tui2

import (
	"context"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestParseCopyCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
		err   bool
	}{
		{"1", 1, false},
		{"5", 5, false},
		{"42", 42, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"", 0, true},
		{" 1 ", 0, true}, // strconv.Atoi rejects leading spaces
	}
	for _, tt := range tests {
		got, err := parseCopyCount(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("parseCopyCount(%q) error = %v, want error=%v", tt.input, err, tt.err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseCopyCount(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalMessagesFiltersCorrectly(t *testing.T) {
	messages := []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleSystem, Content: "system prompt"}, // excluded
		{Role: tauchat.ChatRoleUser, Content: "hello"},
		{Role: tauchat.ChatRoleAssistant, Content: "hi there"},
		{Role: tauchat.ChatRoleTool, Content: "tool result"}, // excluded
		{Role: tauchat.ChatRoleUser, Content: "what about this?"},
		{Role: tauchat.ChatRoleAssistant, Content: ""}, // excluded (empty)
		{Role: tauchat.ChatRoleAssistant, Content: "here's the answer"},
	}
	got := canonicalMessages(messages)
	if len(got) != 4 {
		t.Fatalf("canonicalMessages returned %d messages, want 4: %+v", len(got), got)
	}
	// Order preserved
	if got[0].Content != "hello" {
		t.Errorf("msg[0] = %q, want %q", got[0].Content, "hello")
	}
	if got[1].Content != "hi there" {
		t.Errorf("msg[1] = %q, want %q", got[1].Content, "hi there")
	}
	if got[2].Content != "what about this?" {
		t.Errorf("msg[2] = %q, want %q", got[2].Content, "what about this?")
	}
	if got[3].Content != "here's the answer" {
		t.Errorf("msg[3] = %q, want %q", got[3].Content, "here's the answer")
	}
}

func TestCanonicalMessagesEmpty(t *testing.T) {
	got := canonicalMessages(nil)
	if len(got) != 0 {
		t.Errorf("canonicalMessages(nil) = %d, want 0", len(got))
	}
	got = canonicalMessages([]tauchat.ChatMessage{})
	if len(got) != 0 {
		t.Errorf("canonicalMessages([]) = %d, want 0", len(got))
	}
}

func TestFormatCopyTranscript(t *testing.T) {
	msgs := []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "how do I sort in Go?"},
		{Role: tauchat.ChatRoleAssistant, Content: "Use `slices.Sort` from the standard library:\n\n```go\nslices.Sort(myslice)\n```"},
		{Role: tauchat.ChatRoleUser, Content: "thanks!"},
		{Role: tauchat.ChatRoleAssistant, Content: "You're welcome!"},
	}
	got := formatCopyTranscript(msgs)

	want := "User:\nhow do I sort in Go?\n\n" +
		"Assistant:\nUse `slices.Sort` from the standard library:\n\n" +
		"```go\nslices.Sort(myslice)\n```\n\n" +
		"User:\nthanks!\n\n" +
		"Assistant:\nYou're welcome!"

	if got != want {
		t.Errorf("formatCopyTranscript mismatch.\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestFormatCopyTranscriptSingleMessage(t *testing.T) {
	msgs := []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleAssistant, Content: "hello"},
	}
	got := formatCopyTranscript(msgs)
	want := "Assistant:\nhello"
	if got != want {
		t.Errorf("formatCopyTranscript = %q, want %q", got, want)
	}
}

func TestCmdCopyNoArgs(t *testing.T) {
	// Verify the existing bare /copy still works (copies lastAssistantText).
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	// Nothing to copy should notify.
	m.cmdCopy("")
	if m.notification != "nothing to copy" {
		t.Fatalf("notification = %q, want %q", m.notification, "nothing to copy")
	}

	// Set a valid last assistant text.
	m.lastAssistantText = "hello world"
	cmd := m.cmdCopy("")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd when there is text to copy")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected BatchMsg, got %#v", cmd())
	}
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.Kind() != reflect.String || v.String() != "hello world" {
		t.Fatalf("clipboard payload = %#v, want %q", clip, "hello world")
	}
	if m.notification != "copied to clipboard" {
		t.Errorf("notification = %q, want %q", m.notification, "copied to clipboard")
	}
}

func TestCmdCopySession(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	// Empty session.
	m.canonicalMessages = nil
	m.cmdCopy("session")
	if m.notification != "nothing to copy" {
		t.Fatalf("notification = %q, want %q", m.notification, "nothing to copy")
	}

	// Populated session.
	m.canonicalMessages = []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "hello"},
		{Role: tauchat.ChatRoleAssistant, Content: "hi there"},
	}
	cmd := m.cmdCopy("session")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected BatchMsg, got %#v", cmd())
	}
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	got := v.String()
	want := "User:\nhello\n\nAssistant:\nhi there"
	if got != want {
		t.Errorf("clipboard = %q, want %q", got, want)
	}
	if m.notification != "session copied to clipboard" {
		t.Errorf("notification = %q, want %q", m.notification, "session copied to clipboard")
	}
}

func TestCmdCopyAllIsAliasForSession(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.canonicalMessages = []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "test"},
	}
	// "all" should produce the same result as "session".
	cmd := m.cmdCopy("all")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected BatchMsg, got %#v", cmd())
	}
	clip := batch[0]()
	v := reflect.ValueOf(clip)
	if v.String() != "User:\ntest" {
		t.Errorf("clipboard = %q, want %q", v.String(), "User:\ntest")
	}
	if m.notification != "session copied to clipboard" {
		t.Errorf("notification = %q, want %q", m.notification, "session copied to clipboard")
	}
}

func TestCmdCopyN(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)

	m.canonicalMessages = []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "msg 1"},
		{Role: tauchat.ChatRoleAssistant, Content: "msg 2"},
		{Role: tauchat.ChatRoleUser, Content: "msg 3"},
		{Role: tauchat.ChatRoleAssistant, Content: "msg 4"},
		{Role: tauchat.ChatRoleUser, Content: "msg 5"},
	}

	tests := []struct {
		n    string
		want string
	}{
		{"1", "User:\nmsg 5"},
		{"2", "Assistant:\nmsg 4\n\nUser:\nmsg 5"},
		{"3", "User:\nmsg 3\n\nAssistant:\nmsg 4\n\nUser:\nmsg 5"},
		{"5", "User:\nmsg 1\n\nAssistant:\nmsg 2\n\nUser:\nmsg 3\n\nAssistant:\nmsg 4\n\nUser:\nmsg 5"},
		// Excessive N should clamp to available messages.
		{"10", "User:\nmsg 1\n\nAssistant:\nmsg 2\n\nUser:\nmsg 3\n\nAssistant:\nmsg 4\n\nUser:\nmsg 5"},
	}

	for _, tt := range tests {
		m.notification = "" // reset
		cmd := m.cmdCopy(tt.n)
		if cmd == nil {
			t.Fatalf("cmdCopy(%q) returned nil", tt.n)
		}
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) == 0 {
			t.Fatalf("cmdCopy(%q): expected BatchMsg, got %#v", tt.n, cmd())
		}
		clip := batch[0]()
		v := reflect.ValueOf(clip)
		if v.String() != tt.want {
			t.Errorf("cmdCopy(%q): clipboard = %q, want %q", tt.n, v.String(), tt.want)
		}
	}
}

func TestCmdCopyInvalidN(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.canonicalMessages = []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "hello"},
	}

	for _, bad := range []string{"0", "-1", "abc"} {
		m.notification = ""
		cmd := m.cmdCopy(bad)
		// When N is invalid, cmdCopy should still return a notification Cmd
		// (the notification is set synchronously, and the Cmd is the 4-second
		// tick; we don't need to drain it — just check m.notification).
		if cmd == nil {
			t.Errorf("cmdCopy(%q) returned nil, expected notification Cmd", bad)
		}
		if m.notification == "" {
			t.Errorf("cmdCopy(%q): notification empty, expected error message", bad)
		}
	}
}

func TestCmdCopyEmptyHistory(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.canonicalMessages = nil

	// All variants should notify "nothing to copy".
	for _, arg := range []string{"", "session", "all", "3"} {
		m.notification = ""
		m.cmdCopy(arg)
		if m.notification != "nothing to copy" {
			t.Errorf("cmdCopy(%q) with empty history: notification = %q, want %q", arg, m.notification, "nothing to copy")
		}
	}
}

//nolint:unused // used by copy command tests
func newTestModelCopy(rt tauchat.ChatRuntime) *model {
	return newModel(context.Background(), rt, nil, "sess", "gpt", "openai", nil, nil, true, "medium", false, nil, "", false)
}
