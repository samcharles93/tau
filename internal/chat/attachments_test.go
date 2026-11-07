package chat

import (
	"testing"
	"time"
)

func TestImagePlaceholder(t *testing.T) {
	if got := ImagePlaceholder(1); got != "[Image #1]" {
		t.Fatalf("ImagePlaceholder(1) = %q", got)
	}
	if got := ImagePlaceholder(12); got != "[Image #12]" {
		t.Fatalf("ImagePlaceholder(12) = %q", got)
	}
}

func TestFindImagePlaceholder(t *testing.T) {
	s := "edit [Image #2] like this"
	start, end := FindImagePlaceholder(s)
	if start < 0 {
		t.Fatal("expected a match")
	}
	if s[start:end] != "[Image #2]" {
		t.Fatalf("span = %q, want [Image #2]", s[start:end])
	}
	if start, end := FindImagePlaceholder("no refs here"); start != -1 || end != -1 {
		t.Fatalf("expected no match, got %d,%d", start, end)
	}
}

func TestImagePlaceholderRefs(t *testing.T) {
	refs := ImagePlaceholderRefs("a [Image #2] b [Image #1] c [Image #2] d [Image #0]")
	want := []string{"[Image #2]", "[Image #1]"}
	if len(refs) != len(want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("refs = %v, want %v", refs, want)
		}
	}
}

func TestResolveImageAttachments(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	atts := []ChatAttachment{
		{Placeholder: "[Image #1]", MediaType: "image/png", Data: png},
		{Placeholder: "[Image #2]", MediaType: "image/jpeg", Data: []byte{0xFF, 0xD8}},
		{Placeholder: "[Image #3]", MediaType: "image/png", Data: png}, // not referenced
		{Placeholder: "", MediaType: "image/png", Data: png},           // no placeholder
		{Placeholder: "[Image #4]", MediaType: "image/png"},            // no data
	}
	// Order follows first appearance in the text, not the attachment slice.
	got := ResolveImageAttachments("look at [Image #2] then [Image #1]", atts)
	if len(got) != 2 {
		t.Fatalf("got %d attachments, want 2: %+v", len(got), got)
	}
	if got[0].Placeholder != "[Image #2]" || got[1].Placeholder != "[Image #1]" {
		t.Fatalf("order = %q, %q", got[0].Placeholder, got[1].Placeholder)
	}

	if got := ResolveImageAttachments("no refs", atts); got != nil {
		t.Fatalf("expected nil for text without refs, got %+v", got)
	}
	// Deleting the placeholder from the text detaches the image.
	if got := ResolveImageAttachments("look at [Image #1]", atts); len(got) != 1 || got[0].Placeholder != "[Image #1]" {
		t.Fatalf("got %+v, want only [Image #1]", got)
	}
}

func TestBeginTurnWithAttachments(t *testing.T) {
	s := &ChatSessionState{Status: ChatSessionIdle}
	png := []byte{0x89, 'P', 'N', 'G'}
	atts := []ChatAttachment{
		{Placeholder: "[Image #1]", MediaType: "image/png", Data: png},
		{Placeholder: "[Image #2]", MediaType: "image/png", Data: png}, // detached
	}
	err := s.BeginTurnWithAttachments("r1", "what is in [Image #1]?", atts, time.Now())
	if err != nil {
		t.Fatalf("BeginTurnWithAttachments() error = %v", err)
	}
	if len(s.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(s.Messages))
	}
	msg := s.Messages[0]
	if msg.Role != ChatRoleUser || msg.Content != "what is in [Image #1]?" {
		t.Fatalf("message = %+v", msg)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Placeholder != "[Image #1]" {
		t.Fatalf("attachments = %+v, want only [Image #1]", msg.Attachments)
	}
}

func TestRequestMessagesMergesAttachmentsOfConsecutiveUserMessages(t *testing.T) {
	s := ChatSessionState{
		SystemPrompt: "sys",
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Content: "first [Image #1]", Attachments: []ChatAttachment{{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{1}}}},
			{Role: ChatRoleUser, Content: "second [Image #2]", Attachments: []ChatAttachment{{Placeholder: "[Image #2]", MediaType: "image/png", Data: []byte{2}}}},
			{Role: ChatRoleAssistant, Content: "answer"},
		},
	}
	msgs := s.RequestMessages()
	// system + merged user + assistant
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	merged := msgs[1]
	if merged.Content != "first [Image #1]\n\nsecond [Image #2]" {
		t.Fatalf("merged content = %q", merged.Content)
	}
	if len(merged.Attachments) != 2 {
		t.Fatalf("merged attachments = %+v, want both", merged.Attachments)
	}
}
