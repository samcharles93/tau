package app

import (
	"context"
	"strings"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

// TestDynamicStreamerNoSelection verifies the dynamic streamer returns a
// friendly, actionable error (rather than a nil-provider panic) when the
// session has no provider or model selected.
func TestDynamicStreamerNoSelection(t *testing.T) {
	pr := newProviderRuntime(newRuntimeForProviders(nil, false), nil, false)
	streamer := buildDynamicStreamer(pr)

	_, err := streamer.StreamChatCompletionFull(
		context.Background(),
		tauchat.ChatSessionState{}, // no provider, no model
		"",
		nil,
		tauchat.StreamCallbacks{},
	)
	if err == nil {
		t.Fatal("expected error when no model is selected")
	}
	if !strings.Contains(err.Error(), "/provider") || !strings.Contains(err.Error(), "/model") {
		t.Fatalf("expected guidance toward /provider and /model, got: %v", err)
	}
}

// TestAggregateModelRefsTagsProvider verifies that models discovered across
// multiple providers are each tagged with the provider they came from, which is
// what lets /model switch providers live.
func TestAggregateModelRefsTagsProvider(t *testing.T) {
	providers := []tauconfig.ProviderConfig{
		{
			Name:    "alpha",
			BaseURL: "https://alpha.example/v1",
			Models:  []tauconfig.ModelConfig{{ID: "alpha-1"}},
		},
		{
			Name:    "beta",
			BaseURL: "https://beta.example/v1",
			Models:  []tauconfig.ModelConfig{{ID: "beta-1"}, {ID: "beta-2"}},
		},
	}
	rt := newRuntimeForProviders(providers, false)

	refs := aggregateModelRefs(context.Background(), rt, false, providers)

	byID := make(map[string]string, len(refs))
	for _, r := range refs {
		byID[r.ID] = r.Provider
	}
	for id, wantProvider := range map[string]string{
		"alpha-1": "alpha",
		"beta-1":  "beta",
		"beta-2":  "beta",
	} {
		if got := byID[id]; got != wantProvider {
			t.Errorf("model %q: provider tag = %q, want %q", id, got, wantProvider)
		}
	}
}

func TestBuildToolCallsUsesPresentSparseIndexes(t *testing.T) {
	calls := buildToolCalls(map[int]*assembledToolCall{
		1: {
			id:        "toolu_1",
			name:      "find",
			arguments: `{"pattern":"*.go"}`,
		},
	})

	if len(calls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(calls))
	}
	if calls[0].ID != "toolu_1" {
		t.Fatalf("tool call ID = %q, want toolu_1", calls[0].ID)
	}
	if calls[0].Function.Name != "find" {
		t.Fatalf("tool call name = %q, want find", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"pattern":"*.go"}` {
		t.Fatalf("tool call arguments = %q", calls[0].Function.Arguments)
	}
}
