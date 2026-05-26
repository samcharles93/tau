package platform

import (
	"context"
	"testing"

	"github.com/samcharles93/tau/internal/config"
)

func TestResolveBearerTokenAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TEST_PROVIDER_TOKEN", "secret")
	provider := config.ProviderConfig{
		Name:    "test",
		BaseURL: "https://provider.example",
		Auth:    config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: "TEST_PROVIDER_TOKEN"},
	}
	token, err := ResolveBearerToken(context.Background(), provider, false)
	if err != nil {
		t.Fatalf("ResolveBearerToken() error = %v", err)
	}
	if token != "secret" {
		t.Fatalf("token = %q, want secret", token)
	}
}

func TestResolveBearerTokenAPIKeyLiteralFallback(t *testing.T) {
	provider := config.ProviderConfig{
		Name:    "test",
		BaseURL: "https://provider.example",
		Auth: config.AuthConfig{
			Type:      config.AuthTypeAPIKey,
			APIKeyEnv: "MISSING_TEST_PROVIDER_TOKEN",
			APIKey:    "literal",
		},
	}
	token, err := ResolveBearerToken(context.Background(), provider, false)
	if err != nil {
		t.Fatalf("ResolveBearerToken() error = %v", err)
	}
	if token != "literal" {
		t.Fatalf("token = %q, want literal", token)
	}
}

func TestResolveBearerTokenNone(t *testing.T) {
	provider := config.ProviderConfig{
		Name:    "local",
		BaseURL: "http://localhost:11434",
		Auth:    config.AuthConfig{Type: config.AuthTypeNone},
	}
	token, err := ResolveBearerToken(context.Background(), provider, false)
	if err != nil {
		t.Fatalf("ResolveBearerToken() error = %v", err)
	}
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
}
