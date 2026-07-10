package cli

import (
	"testing"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestUnavailableProviderErrorExplainsMissingEnvVar(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{
			Config:    config.ProviderConfig{Name: "anthropic"},
			Source:    providers.SourceEnv,
			Available: false,
			Message:   "ANTHROPIC_API_KEY is not set",
		},
	}

	got := unavailableProviderError(resolved, "anthropic", "")
	want := `provider "anthropic" is configured but not currently usable: ANTHROPIC_API_KEY is not set`
	if got != want {
		t.Errorf("unavailableProviderError() = %q, want %q", got, want)
	}
}

func TestUnavailableProviderErrorFallsBackToDefaultProvider(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "anthropic"}, Available: false, Message: "ANTHROPIC_API_KEY is not set"},
	}

	// No --provider flag passed (requested == ""), so it should fall back to
	// checking cfg.DefaultProvider instead.
	got := unavailableProviderError(resolved, "", "anthropic")
	if got == "" {
		t.Fatal("expected a message when falling back to the default provider")
	}
}

func TestUnavailableProviderErrorEmptyForUnknownName(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "anthropic"}, Available: false, Message: "ANTHROPIC_API_KEY is not set"},
	}

	// A genuinely unrecognized name must not match anything, so the caller
	// falls back to the generic "unknown provider" error instead of a
	// confusing unrelated message.
	got := unavailableProviderError(resolved, "totally-made-up", "")
	if got != "" {
		t.Errorf("unavailableProviderError() = %q, want empty for an unrecognized name", got)
	}
}

func TestUnavailableProviderErrorEmptyWhenAvailable(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "openai"}, Available: true},
	}

	// An available provider should never produce an "unavailable" message —
	// if ResolveProvider still failed for it, that's a genuine bug elsewhere,
	// not a missing-credential story.
	got := unavailableProviderError(resolved, "openai", "")
	if got != "" {
		t.Errorf("unavailableProviderError() = %q, want empty for an available provider", got)
	}
}

func TestUnavailableProviderErrorDefaultsMessageWhenEmpty(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "mystery"}, Available: false, Message: ""},
	}

	got := unavailableProviderError(resolved, "mystery", "")
	want := `provider "mystery" is configured but not currently usable: credential not available`
	if got != want {
		t.Errorf("unavailableProviderError() = %q, want %q", got, want)
	}
}
