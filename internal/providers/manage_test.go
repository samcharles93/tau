package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sandboxConfigDir points config.Dir() (and therefore StatePath()) at a fresh
// temp dir so Manage tests never touch the real user's auth.yaml.
func sandboxConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
}

func TestManageToggleEnablesThenDisables(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(fakeEnv(nil)) // DEEPSEEK_API_KEY unset -> keyless-style enable

	enabled, warning, err := m.Toggle("deepseek")
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, "DEEPSEEK_API_KEY is not set", warning)

	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("deepseek"))

	enabled, warning, err = m.Toggle("deepseek")
	require.NoError(t, err)
	assert.False(t, enabled)
	assert.Empty(t, warning)

	state, err = LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsDisabled("deepseek"))
}

func TestManageToggleEnvActiveDisablesWithoutWarning(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "sk-d"}))

	enabled, warning, err := m.Toggle("deepseek")
	require.NoError(t, err)
	assert.False(t, enabled, "env-active provider toggles off first")
	assert.Empty(t, warning)

	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsDisabled("deepseek"))
}

func TestManageToggleRejectsOAuthProvider(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)

	_, _, err := m.Toggle("github-copilot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OAuth")
}

func TestManageToggleRejectsManagedKeyProvider(t *testing.T) {
	sandboxConfigDir(t)
	state, err := LoadState()
	require.NoError(t, err)
	state.SetAPIKey("deepseek", "sk-managed")
	require.NoError(t, state.Save())

	m := NewManage(fakeEnv(nil))
	_, _, err = m.Toggle("deepseek")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stored API key")
}

func TestManageToggleUnknownProvider(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)
	_, _, err := m.Toggle("not-a-real-provider")
	require.Error(t, err)
}

func TestManageLoginCompleteStoresCredsAndEnables(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)

	err := m.LoginComplete("anthropic", OAuthCredentials{Access: "tok"})
	require.Error(t, err, "anthropic is an API-key provider, not OAuth")

	err = m.LoginComplete("github-copilot", OAuthCredentials{Access: "ghu_tok"})
	require.NoError(t, err)

	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("github-copilot"))
	creds, ok := state.OAuthFor("github-copilot")
	require.True(t, ok)
	assert.Equal(t, "ghu_tok", creds.Access)
}

func TestManageLoginCompletePropagatesLoadAndSaveErrors(t *testing.T) {
	loadErr := errors.New("load failed")
	m := newManage(nil, func() (State, error) {
		return State{}, loadErr
	}, func(*State) error {
		t.Fatal("save should not run after a load failure")
		return nil
	})
	err := m.LoginComplete("github-copilot", OAuthCredentials{Access: "token"})
	require.ErrorIs(t, err, loadErr)
	assert.Contains(t, err.Error(), "load provider state")

	saveErr := errors.New("save failed")
	m = newManage(nil, func() (State, error) {
		return State{Version: stateVersion}, nil
	}, func(*State) error {
		return saveErr
	})
	err = m.LoginComplete("github-copilot", OAuthCredentials{Access: "token"})
	require.ErrorIs(t, err, saveErr)
	assert.Contains(t, err.Error(), "save provider state")
}

func TestManageLogoutClearsBothCredentialKindsAndDisables(t *testing.T) {
	sandboxConfigDir(t)
	state, err := LoadState()
	require.NoError(t, err)
	state.Enable("deepseek")
	state.SetAPIKey("deepseek", "sk-managed")
	state.SetOAuth("deepseek", OAuthCredentials{Access: "stray-oauth-cred"})
	require.NoError(t, state.Save())

	m := NewManage(nil)
	require.NoError(t, m.Logout("deepseek"))

	reloaded, err := LoadState()
	require.NoError(t, err)
	assert.True(t, reloaded.IsDisabled("deepseek"))
	assert.False(t, reloaded.IsEnabled("deepseek"))
	_, hasKey := reloaded.APIKeyFor("deepseek")
	assert.False(t, hasKey)
	_, hasOAuth := reloaded.OAuthFor("deepseek")
	assert.False(t, hasOAuth)
}

func TestManageLogoutUnknownProvider(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)
	err := m.Logout("not-a-real-provider")
	require.Error(t, err)
}

func TestManageStoreAPIKeyEnablesAndPersists(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)

	require.NoError(t, m.StoreAPIKey("deepseek", "sk-test"))

	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("deepseek"))
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-test", key)
}

func TestManageStoreAPIKeyRejectsEmptyKey(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)
	require.Error(t, m.StoreAPIKey("deepseek", "   "))
}

func TestManageStoreAPIKeyRejectsNonAPIKeyProvider(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)
	require.Error(t, m.StoreAPIKey("github-copilot", "ghu_x"))
}

func TestManageEnableIsIdempotentUnlikeToggle(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(fakeEnv(nil))

	require.NoError(t, m.Enable("ollama"))
	state, err := LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("ollama"))

	// A second Enable call must not flip it back off, unlike Toggle.
	require.NoError(t, m.Enable("ollama"))
	state, err = LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("ollama"))
}

func TestManageEnableRejectsOAuthProvider(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(nil)
	require.Error(t, m.Enable("github-copilot"))
}

func TestManageEffectiveUsesInjectedEnv(t *testing.T) {
	sandboxConfigDir(t)
	m := NewManage(fakeEnv(map[string]string{"DEEPSEEK_API_KEY": "sk-d"}))
	state, err := LoadState()
	require.NoError(t, err)
	state.Enable("deepseek")
	require.NoError(t, state.Save())

	cfg, resolved, err := m.Effective(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.Providers)

	found := false
	for _, p := range resolved {
		if p.Config.Name == "deepseek" {
			found = true
			assert.True(t, p.Available)
		}
	}
	assert.True(t, found)
}
