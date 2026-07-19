package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestListProvidersKeepsConfigAndManagedSourcesDistinct(t *testing.T) {
	sandboxConfigDir(t)
	require.NoError(t, config.SaveDefaultProviderAndModel("", "", ""))
	configPath := config.GlobalPath()
	require.NoError(t, os.WriteFile(configPath, []byte(`providers:
  - name: mistral
    base_url: https://mistral.example
    auth:
      type: none
`), 0o600))
	state, err := providers.LoadState()
	require.NoError(t, err)
	state.SetAPIKey("deepseek", "sk-managed")
	require.NoError(t, state.Save())

	statuses, err := listProviders(func(string) string { return "" })
	require.NoError(t, err)
	byID := make(map[string]ProviderStatus, len(statuses))
	for _, status := range statuses {
		byID[status.ID] = status
	}
	assert.Equal(t, "configured", byID["mistral"].Status)
	assert.Equal(t, "config", byID["mistral"].Source)
	assert.Equal(t, "enabled", byID["deepseek"].Status)
	assert.Equal(t, "managed", byID["deepseek"].Source)
}

func TestRunProviderLoginStoresAPIKey(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, alwaysValid)
	prompter := &fakeSetupPrompter{secretQueue: []string{"", "sk-test-key"}}

	result, err := RunProviderLogin(context.Background(), ProviderLoginOptions{
		ProviderID: "deepseek",
		Prompter:   prompter,
		Stdout:     io.Discard,
	})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", result.ProviderID)
	assert.Len(t, prompter.secretPrompts, 2)

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-test-key", key)
}

// TestRunProviderLoginPassesInsecureToValidation guards against a real bug:
// ProviderLoginOptions.Insecure was silently dropped on its way into the
// live API-key validation call, so `tau provider login --insecure` behind a
// TLS-intercepting proxy behaved identically to a plain `tau provider
// login` and always validated with insecure=false.
func TestRunProviderLoginPassesInsecureToValidation(t *testing.T) {
	sandboxConfigDir(t)
	var gotInsecure bool
	orig := validateAPIKey
	validateAPIKey = func(_ context.Context, _ providers.CatalogEntry, _ string, insecure bool) apiKeyValidationResult {
		gotInsecure = insecure
		return apiKeyValidationResult{outcome: apiKeyValid}
	}
	t.Cleanup(func() { validateAPIKey = orig })
	prompter := &fakeSetupPrompter{secretQueue: []string{"", "sk-test-key"}}

	_, err := RunProviderLogin(context.Background(), ProviderLoginOptions{
		ProviderID: "deepseek",
		Prompter:   prompter,
		Stdout:     io.Discard,
		Insecure:   true,
	})
	require.NoError(t, err)
	assert.True(t, gotInsecure, "RunProviderLogin should pass Insecure through to the live validation call")
}

func TestRunProviderLoginCompletesOAuthDeviceFlow(t *testing.T) {
	sandboxConfigDir(t)
	var output bytes.Buffer

	result, err := RunProviderLogin(context.Background(), ProviderLoginOptions{
		ProviderID:       "openai-codex",
		EnterpriseDomain: "ignored.example",
		Prompter:         &fakeSetupPrompter{},
		Stdout:           &output,
		loginOAuth: func(_ context.Context, providerID string, opts providers.LoginOptions) (providers.OAuthCredentials, error) {
			assert.Equal(t, "openai-codex", providerID)
			opts.OnDeviceCode(providers.DeviceCode{VerificationURI: "https://example.test/device", UserCode: "ABCD-1234"})
			return providers.OAuthCredentials{Access: "oauth-token"}, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "openai-codex", result.ProviderID)
	assert.Contains(t, output.String(), "ABCD-1234")

	state, err := providers.LoadState()
	require.NoError(t, err)
	creds, ok := state.OAuthFor("openai-codex")
	require.True(t, ok)
	assert.Equal(t, "oauth-token", creds.Access)
}

func TestLogoutProviderClearsManagedCredentials(t *testing.T) {
	sandboxConfigDir(t)
	state, err := providers.LoadState()
	require.NoError(t, err)
	state.SetAPIKey("openai", "sk-test-key")
	state.SetOAuth("openai", providers.OAuthCredentials{Access: "stray-token"})
	require.NoError(t, state.Save())

	displayName, err := LogoutProvider("openai")
	require.NoError(t, err)
	assert.Equal(t, "OpenAI", displayName)

	state, err = providers.LoadState()
	require.NoError(t, err)
	_, hasKey := state.APIKeyFor("openai")
	_, hasOAuth := state.OAuthFor("openai")
	assert.False(t, hasKey)
	assert.False(t, hasOAuth)
	assert.True(t, state.IsDisabled("openai"))
}
