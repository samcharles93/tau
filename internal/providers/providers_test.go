package providers

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func TestOAuthCatalogEntries(t *testing.T) {
	copilot, ok := Lookup("github-copilot")
	require.True(t, ok)
	assert.Equal(t, AuthOAuth, copilot.Auth)
	assert.Equal(t, "github-copilot", copilot.OAuthHandler)
	assert.NotEmpty(t, copilot.Headers["Copilot-Integration-Id"])

	codex, ok := Lookup("openai-codex")
	require.True(t, ok)
	assert.Equal(t, AuthOAuth, codex.Auth)
	assert.Equal(t, "openai-codex", codex.OAuthHandler)
	assert.Equal(t, "openai-codex", codex.Class)
}

func TestDeepSeekCatalogUsesNativeProviderClass(t *testing.T) {
	entry, ok := Lookup("deepseek")
	require.True(t, ok)
	assert.Equal(t, "https://api.deepseek.com", entry.BaseURL)
	assert.Equal(t, "deepseek", entry.Class)
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
	s.SetOAuth("anthropic", OAuthCredentials{Access: "tok", Refresh: "r", Expires: 0, Extra: map[string]string{"base_url": "https://example.test"}})
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
	assert.Equal(t, "https://example.test", creds.Extra["base_url"])

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

func TestStateAPIKeyRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	s, err := loadStateFrom(path)
	require.NoError(t, err)

	s.SetAPIKey("deepseek", "sk-deepseek-test")
	require.NoError(t, s.Save())

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	reloaded, err := loadStateFrom(path)
	require.NoError(t, err)
	key, ok := reloaded.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-deepseek-test", key)

	// A managed key does not enable/disable the provider - those lists are
	// only for suppressing auto-detection of env-var-backed providers.
	assert.False(t, reloaded.IsEnabled("deepseek"))
	assert.False(t, reloaded.IsDisabled("deepseek"))

	reloaded.RemoveAPIKey("deepseek")
	require.NoError(t, reloaded.Save())

	again, err := loadStateFrom(path)
	require.NoError(t, err)
	_, ok = again.APIKeyFor("deepseek")
	assert.False(t, ok)
}

func TestStateAPIKeyEmptyMapOmittedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	s, err := loadStateFrom(path)
	require.NoError(t, err)

	s.SetAPIKey("deepseek", "sk-test")
	s.RemoveAPIKey("deepseek")
	require.NoError(t, s.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "api_keys")
}

func TestStoreAPIKeyNeverExposesKeyInErrorsOrLogs(t *testing.T) {
	secret := "sk-sensitive-provider-key"
	path := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	t.Setenv("TAU_CONFIG_DIR", path)

	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })

	err := NewManage(nil).StoreAPIKey("deepseek", secret)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, logs.String(), secret)
}

func TestStateConcurrentSavesAlwaysProduceValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	initial := State{Version: stateVersion, path: path}
	require.NoError(t, initial.Save())

	const writers = 24
	start := make(chan struct{})
	releaseAfterProbe := make(chan struct{})
	errs := make(chan error, writers)
	var firstSaves sync.WaitGroup
	firstSaves.Add(writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			for attempt := range 10 {
				state := State{Version: stateVersion, path: path}
				state.SetAPIKey("deepseek", strings.Repeat("x", i+1))
				if err := state.Save(); err != nil {
					if attempt == 0 {
						firstSaves.Done()
					}
					errs <- err
					return
				}
				if attempt == 0 {
					firstSaves.Done()
					<-releaseAfterProbe
				}
			}
			errs <- nil
		})
	}

	close(start)
	firstSaves.Wait()
	observed, observedErr := loadStateFrom(path)
	close(releaseAfterProbe)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, observedErr, "state must remain valid after simultaneous writers")
	_, hasObservedKey := observed.APIKeyFor("deepseek")
	require.True(t, hasObservedKey)
	final, err := loadStateFrom(path)
	require.NoError(t, err)
	key, ok := final.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.NotEmpty(t, key)
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

	got := Resolve(context.Background(), cfg, state, env)

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
	assert.Equal(t, "GROQ_API_KEY is not set", byName["groq"].Message, "unavailable API-key provider must explain which env var is missing")

	_, hasTogether := byName["together"]
	assert.False(t, hasTogether, "key present but disabled -> excluded")

	assert.Equal(t, SourceOAuth, byName["anthropic"].Source)
	assert.True(t, byName["anthropic"].Available)
	assert.Equal(t, "ctok", byName["anthropic"].Config.Auth.APIKey)
}

func TestResolveManagedKeyPrecedence(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{
		{Name: "mistral", Auth: config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: "MISTRAL_API_KEY"}},
	}}
	state := State{
		Disabled: []string{"groq"}, // suppresses groq's env auto-detection only
		APIKeys: map[string]string{
			"deepseek":   "sk-deepseek-managed",   // no env var -> managed wins
			"openrouter": "sk-openrouter-managed", // env var also set -> env wins
			"groq":       "sk-groq-managed",       // env disabled -> managed key still independent
			"mistral":    "sk-mistral-managed",    // config-declared -> config wins outright
		},
	}
	env := fakeEnv(map[string]string{
		"OPENROUTER_API_KEY": "sk-openrouter-env",
		"GROQ_API_KEY":       "sk-groq-env",
	})

	got := Resolve(context.Background(), cfg, state, env)
	byName := map[string]ResolvedProvider{}
	for _, p := range got {
		byName[p.Config.Name] = p
	}

	assert.Equal(t, SourceManaged, byName["deepseek"].Source)
	assert.True(t, byName["deepseek"].Available)
	assert.Equal(t, "sk-deepseek-managed", byName["deepseek"].Config.Auth.APIKey)

	assert.Equal(t, SourceEnv, byName["openrouter"].Source, "env must win over a managed key for the same provider")
	assert.Equal(t, 1, countByName(got, "openrouter"), "env winning must not also emit a managed duplicate")

	assert.Equal(t, SourceManaged, byName["groq"].Source, "Disabled only suppresses env auto-detection, not a managed key")
	assert.True(t, byName["groq"].Available)

	assert.Equal(t, SourceConfig, byName["mistral"].Source, "hand-written config wins over env and managed both")
	assert.Equal(t, 1, countByName(got, "mistral"), "config winning must not also emit a managed duplicate")

	envIdx, managedIdx := -1, -1
	for i, p := range got {
		if p.Config.Name == "openrouter" && envIdx == -1 {
			envIdx = i
		}
		if p.Config.Name == "deepseek" && managedIdx == -1 {
			managedIdx = i
		}
	}
	require.NotEqual(t, -1, envIdx)
	require.NotEqual(t, -1, managedIdx)
	assert.Less(t, envIdx, managedIdx, "managed providers must sort after env providers")
}

func countByName(resolved []ResolvedProvider, name string) int {
	n := 0
	for _, p := range resolved {
		if p.Config.Name == name {
			n++
		}
	}
	return n
}

func TestMenuManagedKey(t *testing.T) {
	cfg := config.Config{}
	state := State{
		APIKeys: map[string]string{
			"deepseek":   "sk-deepseek-managed",
			"openrouter": "sk-openrouter-managed",
		},
	}
	env := fakeEnv(map[string]string{"OPENROUTER_API_KEY": "sk-openrouter-env"})

	menu := Menu(cfg, state, env)
	byID := map[string]MenuEntry{}
	for _, e := range menu {
		byID[e.ID] = e
	}

	assert.Equal(t, SourceManaged, byID["deepseek"].Source)
	assert.True(t, byID["deepseek"].Enabled)
	assert.True(t, byID["deepseek"].Available)
	assert.Equal(t, "stored key", byID["deepseek"].Message)

	assert.Equal(t, SourceEnv, byID["openrouter"].Source, "env must win over a managed key in the menu too")
	assert.Empty(t, byID["openrouter"].Message)
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
