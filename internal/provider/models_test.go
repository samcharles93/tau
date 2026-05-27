package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/tau/internal/config"
)

func TestDiscoverModelsOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"alpha"},{"id":"beta"}]}`))
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
	}, "token", false)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 || models[0].ID != "alpha" || models[1].URL != server.URL || !models[1].Ready {
		t.Fatalf("models = %#v", models)
	}
}

func TestDiscoverModelsSupplementsWithConfiguredModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"remote"},{"id":"deepseek-v4-pro"}]}`))
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), config.ProviderConfig{
		Name:    "deepseek",
		BaseURL: server.URL,
		Auth:    config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: "DEEPSEEK_API_KEY"},
		Models: []config.ModelConfig{{
			ID:   "deepseek-v4-pro",
			Name: "DeepSeek V4 Pro",
			Compat: config.CompatConfig{
				MaxTokensField: "max_completion_tokens",
			},
		}},
	}, "token", false)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v, want configured plus remote without duplicate", models)
	}
	if models[0].ID != "deepseek-v4-pro" || models[0].Config.Compat.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("configured model metadata was not preserved: %#v", models[0])
	}
	if models[1].ID != "remote" {
		t.Fatalf("remote model = %#v, want remote", models[1])
	}
}

func TestDiscoverModelsFallsBackToConfiguredModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), config.ProviderConfig{
		Name:    "deepseek",
		BaseURL: server.URL,
		Auth:    config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: "DEEPSEEK_API_KEY"},
		Models: []config.ModelConfig{{
			ID: "deepseek-v4-pro",
		}},
	}, "token", false)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "deepseek-v4-pro" || !models[0].Ready {
		t.Fatalf("models = %#v, want configured ready model", models)
	}
}
