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
