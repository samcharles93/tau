package app

import (
	"testing"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/provider"
)

func TestPickModelUsesConfiguredModelMetadata(t *testing.T) {
	model, err := pickModel([]provider.Model{{
		ID:    "deepseek-v4-pro",
		URL:   "https://api.deepseek.com",
		Ready: true,
		Config: config.ModelConfig{
			ID: "deepseek-v4-pro",
			Compat: config.CompatConfig{
				MaxTokensField: "max_completion_tokens",
				ExtraBody: map[string]any{
					"thinking": map[string]any{"type": "enabled"},
				},
			},
		},
	}}, "", "deepseek-v4-pro", "https://api.deepseek.com")
	if err != nil {
		t.Fatalf("pickModel() error = %v", err)
	}
	if model.ID != "deepseek-v4-pro" || model.Config.Compat.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("model = %#v, want configured metadata", model)
	}
}
