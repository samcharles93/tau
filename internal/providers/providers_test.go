package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/config"
)

func fakeEnv(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestCatalogDetectEnvVar(t *testing.T) {
	entry, ok := Lookup("together")
	require.True(t, ok)

	// Falls through to the second candidate var.
	name, present := entry.DetectEnvVar(fakeEnv(map[string]string{"TOGETHERAI_API_KEY": "sk-x"}))
	assert.True(t, present)
	assert.Equal(t, "TOGETHERAI_API_KEY", name)

	// None set: returns first candidate for display, present=false.
	name, present = entry.DetectEnvVar(fakeEnv(nil))
	assert.False(t, present)
	assert.Equal(t, "TOGETHER_API_KEY", name)
}

func TestStateRoundTripAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	s, err := loadStateFrom(path)
	require.NoError(t, err)
	assert.Equal(t, stateVersion, s.Version)

	s.Enable("openrouter")
	s.Enable("openrouter") // idempotent
	s.Enable("deepseek")
	s.Disable("openai")
	s.SetOAuth("anthropic", OAuthCredentials{Access: "tok", Refresh: "r", Expires: 0})
	require.NoError(t, s.Save())

	// File should be 0600 (not world-readable: credentials live here).
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	reloaded, err := loadStateFrom(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek", "openrouter"}, reloaded.Enabled) // sorted, deduped
	assert.Equal(t, []string{"openai"}, reloaded.Disabled)
	assert.True(t, reloaded.IsEnabled("openrouter"))
	assert.True(t, reloaded.IsDisabled("openai"))

	creds, ok := reloaded.OAuthFor("anthropic")
	require.True(t, ok)
	assert.Equal(t, "tok", creds.Access)

	reloaded.Disable("openrouter")
	reloaded.RemoveOAuth("anthropic")
	require.NoError(t, reloaded.Save())

	again, err := loadStateFrom(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek"}, again.Enabled)
	assert.True(t, again.IsDisabled("openrouter"))
	_, ok = again.OAuthFor("anthropic")
	assert.False(t, ok)
}

func TestLoadStateMissingFileIsEmpty(t *testing.T) {
	s, err := loadStateFrom(filepath.Join(t.TempDir(), "nope.yaml"))
	require.NoError(t, err)
	assert.Empty(t, s.Enabled)
	assert.Equal(t, stateVersion, s.Version)
}

func TestResolvePrecedence(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{
		{Name: "openrouter", BaseURL: "https://custom/v1", Auth: config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: "OPENROUTER_API_KEY"}},
	}}
	state := State{
		Enabled:  []string{"groq"},     // explicit enable, no key -> unavailable
		Disabled: []string{"together"}, // suppress an auto-detected provider
		OAuth:    map[string]OAuthCredentials{"anthropic": {Access: "ctok"}},
	}
	env := fakeEnv(map[string]string{
		"DEEPSEEK_API_KEY": "sk-d", // auto-detected, no explicit enable needed
		"TOGETHER_API_KEY": "sk-t", // present but disabled -> excluded
		// GROQ_API_KEY intentionally unset -> enabled but unavailable.
	})

	got := Resolve(cfg, state, env)

	// Config provider wins over the catalog enable (custom base URL preserved).
	require.GreaterOrEqual(t, len(got), 4)
	assert.Equal(t, "openrouter", got[0].Config.Name)
	assert.Equal(t, SourceConfig, got[0].Source)
	assert.Equal(t, "https://custom/v1", got[0].Config.BaseURL)

	byName := map[string]ResolvedProvider{}
	for _, p := range got {
		byName[p.Config.Name] = p
	}

	// Only one openrouter entry (config, not duplicated as env).
	count := 0
	for _, p := range got {
		if p.Config.Name == "openrouter" {
			count++
		}
	}
	assert.Equal(t, 1, count)

	assert.Equal(t, SourceEnv, byName["deepseek"].Source, "auto-detected from env")
	assert.True(t, byName["deepseek"].Available)

	assert.Equal(t, SourceEnv, byName["groq"].Source)
	assert.False(t, byName["groq"].Available, "enabled but no key -> unavailable")

	_, hasTogether := byName["together"]
	assert.False(t, hasTogether, "key present but disabled -> excluded")

	assert.Equal(t, SourceOAuth, byName["anthropic"].Source)
	assert.True(t, byName["anthropic"].Available)
	assert.Equal(t, "ctok", byName["anthropic"].Config.Auth.APIKey)
}

func TestMenuReflectsState(t *testing.T) {
	cfg := config.Config{}
	state := State{
		Enabled: []string{"openrouter"},
	}
	env := fakeEnv(map[string]string{"OPENROUTER_API_KEY": "x"})

	menu := Menu(cfg, state, env)
	byID := map[string]MenuEntry{}
	for _, e := range menu {
		byID[e.ID] = e
	}

	assert.True(t, byID["openrouter"].Enabled)
	assert.True(t, byID["openrouter"].Available)
	assert.False(t, byID["deepseek"].Enabled)

	// anthropic is an API-key provider; without its key it is neither enabled
	// nor available.
	assert.Equal(t, AuthAPIKey, byID["anthropic"].Auth)
	assert.False(t, byID["anthropic"].Available)

	// Ollama needs no key: available even without an env var.
	assert.True(t, byID["ollama"].Available)
}
