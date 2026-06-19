package ai

import (
	"testing"

	aisdkchat "github.com/samcharles93/ai-sdk/pkg/chat"
)

func TestResolveChatProviderMapping(t *testing.T) {
	tests := []struct {
		name    string
		npm     string
		wantErr bool
	}{
		{"deepseek", "@ai-sdk/deepseek", false},
		{"openai", "@ai-sdk/openai", false},
		{"anthropic", "@ai-sdk/anthropic", false},
		{"unknown", "@ai-sdk/unknown", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ResolveChatProvider(ChatProviderConfig{
				ProviderID: "test",
				ModelID:    "test-model",
				NPM:        tc.npm,
				APIKey:     "sk-test",
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("provider is nil")
			}
			var _ aisdkchat.Provider = p
		})
	}
}
