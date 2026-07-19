package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// sandboxConfigDir points config.Dir()/providers.StatePath() at a fresh temp
// dir so RunSetup tests never touch the real user's config.yaml/auth.yaml.
func sandboxConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("TAU_CONFIG_DIR", t.TempDir())
}

type selectCall struct {
	title   string
	options []SetupOption
}

// fakeSetupPrompter is a scripted SetupPrompter for testing RunSetup without
// a real terminal. selectPick chooses among the options offered on each
// Select call; nil defaults to the first option.
type fakeSetupPrompter struct {
	selectCalls []selectCall
	selectPick  func(call int, options []SetupOption) SetupOption
	selectErr   error

	secretPrompts []string
	secretQueue   []string
	secretErr     error
}

func (f *fakeSetupPrompter) Select(ctx context.Context, title string, options []SetupOption) (SetupOption, error) {
	f.selectCalls = append(f.selectCalls, selectCall{title: title, options: options})
	if f.selectErr != nil {
		return SetupOption{}, f.selectErr
	}
	if len(options) == 0 {
		return SetupOption{}, errors.New("no options to select from")
	}
	if f.selectPick != nil {
		return f.selectPick(len(f.selectCalls), options), nil
	}
	return options[0], nil
}

func (f *fakeSetupPrompter) ReadSecret(ctx context.Context, prompt string) (string, error) {
	f.secretPrompts = append(f.secretPrompts, prompt)
	if f.secretErr != nil {
		return "", f.secretErr
	}
	if len(f.secretQueue) == 0 {
		return "", nil
	}
	v := f.secretQueue[0]
	f.secretQueue = f.secretQueue[1:]
	return v, nil
}

// pickProviderThenFirst returns a selectPick that chooses the option whose
// Value matches want on the first Select call (provider selection) and the
// first option on every subsequent call (model selection).
func pickProviderThenFirst(want string) func(call int, options []SetupOption) SetupOption {
	return func(call int, options []SetupOption) SetupOption {
		if call == 1 {
			for _, o := range options {
				if o.Value == want {
					return o
				}
			}
		}
		return options[0]
	}
}

func TestRunSetupNoPrompterErrors(t *testing.T) {
	sandboxConfigDir(t)
	_, err := RunSetup(context.Background(), RunSetupOptions{})
	require.Error(t, err)
}

func TestRunSetupKeylessProviderPersists(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{selectPick: pickProviderThenFirst("ollama")}

	result, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, "ollama", result.ProviderID)
	assert.Equal(t, "Ollama (local)", result.ProviderName)

	state, err := providers.LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("ollama"))

	cfg, err := tauconfig.LoadConfigAllowEmpty()
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.DefaultProvider)
}

// stubAPIKeyValidation replaces the validateAPIKey seam for the duration of
// a test so RunSetup's live-validation step never makes a real network call.
// fn is called once per ReadSecret entry, in order.
func stubAPIKeyValidation(t *testing.T, fn func(key string) apiKeyValidationResult) {
	t.Helper()
	orig := validateAPIKey
	validateAPIKey = func(_ context.Context, _ providers.CatalogEntry, key string, _ bool) apiKeyValidationResult {
		return fn(key)
	}
	t.Cleanup(func() { validateAPIKey = orig })
}

func alwaysValid(_ string) apiKeyValidationResult {
	return apiKeyValidationResult{outcome: apiKeyValid}
}

func TestRunSetupAPIKeyRejectedRetryThenValidPersists(t *testing.T) {
	sandboxConfigDir(t)
	var validateCalls []string
	stubAPIKeyValidation(t, func(key string) apiKeyValidationResult {
		validateCalls = append(validateCalls, key)
		if key == "sk-bad-key" {
			return apiKeyValidationResult{outcome: apiKeyRejected, err: errors.New("status 401")}
		}
		return apiKeyValidationResult{outcome: apiKeyValid}
	})

	prompter := &fakeSetupPrompter{
		secretQueue: []string{"sk-bad-key", "sk-good-key"},
		selectPick: func(call int, options []SetupOption) SetupOption {
			if call == 1 {
				for _, o := range options {
					if o.Value == "deepseek" {
						return o
					}
				}
			}
			if call == 2 {
				// The rejected-key prompt: choose to retry with a new key.
				for _, o := range options {
					if o.Value == "retry" {
						return o
					}
				}
			}
			return options[0]
		},
	}

	result, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", result.ProviderID)
	assert.Equal(t, []string{"sk-bad-key", "sk-good-key"}, validateCalls, "rejection must trigger a retry, not an immediate store")

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-good-key", key, "the retried, valid key must be the one persisted")
}

func TestRunSetupAPIKeyRejectedOverridePersistsAndDoesNotLeakKey(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, func(string) apiKeyValidationResult {
		return apiKeyValidationResult{outcome: apiKeyRejected, err: errors.New("status 401")}
	})

	prompter := &fakeSetupPrompter{
		secretQueue: []string{"sk-rejected-but-forced"},
		selectPick: func(call int, options []SetupOption) SetupOption {
			if call == 1 {
				for _, o := range options {
					if o.Value == "deepseek" {
						return o
					}
				}
			}
			if call == 2 {
				for _, o := range options {
					if o.Value == "override" {
						return o
					}
				}
			}
			return options[0]
		},
	}

	result, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", result.ProviderID)

	require.Len(t, prompter.selectCalls, 3, "provider, rejection choice, model")
	choicePromptTitle := prompter.selectCalls[1].title
	assert.NotContains(t, choicePromptTitle, "sk-rejected-but-forced", "the rejection prompt must never echo the key")
	assert.Contains(t, choicePromptTitle, "rejected")

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok, "an explicit override must persist the key despite rejection")
	assert.Equal(t, "sk-rejected-but-forced", key)
}

func TestRunSetupAPIKeyRejectedCanceledAtChoiceWritesNothing(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, func(string) apiKeyValidationResult {
		return apiKeyValidationResult{outcome: apiKeyRejected, err: errors.New("status 401")}
	})

	prompter := &fakeSetupPrompter{
		secretQueue: []string{"sk-test-key"},
		selectPick: func(call int, options []SetupOption) SetupOption {
			if call == 1 {
				for _, o := range options {
					if o.Value == "deepseek" {
						return o
					}
				}
			}
			return options[0]
		},
	}
	// Cancel exactly at the rejection-choice prompt (the second Select call).
	calls := 0
	cancelingPrompter := &cancelAtCallPrompter{fakeSetupPrompter: prompter, cancelAtCall: 2, calls: &calls}

	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: cancelingPrompter})
	require.ErrorIs(t, err, ErrSetupCanceled)

	state, err := providers.LoadState()
	require.NoError(t, err)
	_, ok := state.APIKeyFor("deepseek")
	assert.False(t, ok, "canceling at the rejection choice must not persist the rejected key")
}

// cancelAtCallPrompter wraps fakeSetupPrompter and turns the Nth Select call
// into ErrSetupCanceled, so a test can exercise cancellation from inside the
// retry/override choice prompt specifically (never-locked-out guarantee: the
// user can always back out, even mid-validation-choice).
type cancelAtCallPrompter struct {
	*fakeSetupPrompter
	cancelAtCall int
	calls        *int
}

func (c *cancelAtCallPrompter) Select(ctx context.Context, title string, options []SetupOption) (SetupOption, error) {
	*c.calls++
	if *c.calls == c.cancelAtCall {
		return SetupOption{}, ErrSetupCanceled
	}
	return c.fakeSetupPrompter.Select(ctx, title, options)
}

func TestRunSetupAPIKeyInconclusiveProceedPersists(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, func(string) apiKeyValidationResult {
		return apiKeyValidationResult{outcome: apiKeyInconclusive, err: errors.New("dial tcp: connection refused")}
	})

	prompter := &fakeSetupPrompter{
		secretQueue: []string{"sk-unverified-key"},
		selectPick: func(call int, options []SetupOption) SetupOption {
			if call == 1 {
				for _, o := range options {
					if o.Value == "deepseek" {
						return o
					}
				}
			}
			if call == 2 {
				for _, o := range options {
					if o.Value == "proceed" {
						return o
					}
				}
			}
			return options[0]
		},
	}

	result, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", result.ProviderID)

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok, "explicit proceed must persist the key despite an inconclusive check")
	assert.Equal(t, "sk-unverified-key", key)
}

func TestRunSetupAPIKeyInconclusiveRetryThenValidPersists(t *testing.T) {
	sandboxConfigDir(t)
	var validateCalls []string
	stubAPIKeyValidation(t, func(key string) apiKeyValidationResult {
		validateCalls = append(validateCalls, key)
		if key == "sk-first-attempt" {
			return apiKeyValidationResult{outcome: apiKeyInconclusive, err: errors.New("timeout")}
		}
		return apiKeyValidationResult{outcome: apiKeyValid}
	})

	prompter := &fakeSetupPrompter{
		secretQueue: []string{"sk-first-attempt", "sk-second-attempt"},
		selectPick: func(call int, options []SetupOption) SetupOption {
			if call == 1 {
				for _, o := range options {
					if o.Value == "deepseek" {
						return o
					}
				}
			}
			if call == 2 {
				for _, o := range options {
					if o.Value == "retry" {
						return o
					}
				}
			}
			return options[0]
		},
	}

	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-first-attempt", "sk-second-attempt"}, validateCalls)

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-second-attempt", key)
}

func TestRunSetupAPIKeyProviderPersists(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, alwaysValid)
	prompter := &fakeSetupPrompter{
		selectPick:  pickProviderThenFirst("deepseek"),
		secretQueue: []string{"sk-test-key"},
	}

	result, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", result.ProviderID)

	state, err := providers.LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("deepseek"))
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-test-key", key)

	cfg, err := tauconfig.LoadConfigAllowEmpty()
	require.NoError(t, err)
	assert.Equal(t, "deepseek", cfg.DefaultProvider)
}

func TestRunSetupAPIKeyEmptyEntryReprompts(t *testing.T) {
	sandboxConfigDir(t)
	stubAPIKeyValidation(t, alwaysValid)
	prompter := &fakeSetupPrompter{
		selectPick:  pickProviderThenFirst("deepseek"),
		secretQueue: []string{"  ", "", "sk-test-key"},
	}

	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)
	assert.Len(t, prompter.secretPrompts, 3, "empty/whitespace-only entries must re-prompt, not error")

	state, err := providers.LoadState()
	require.NoError(t, err)
	key, ok := state.APIKeyFor("deepseek")
	require.True(t, ok)
	assert.Equal(t, "sk-test-key", key)
}

func TestRunSetupCanceledAtProviderSelectionWritesNothing(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{selectErr: ErrSetupCanceled}

	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.ErrorIs(t, err, ErrSetupCanceled)

	state, err := providers.LoadState()
	require.NoError(t, err)
	assert.Empty(t, state.Enabled)
	assert.Empty(t, state.APIKeys)
}

func TestRunSetupCanceledAtAPIKeyEntryWritesNothing(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{
		selectPick: pickProviderThenFirst("deepseek"),
		secretErr:  ErrSetupCanceled,
	}

	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.ErrorIs(t, err, ErrSetupCanceled)

	state, err := providers.LoadState()
	require.NoError(t, err)
	_, ok := state.APIKeyFor("deepseek")
	assert.False(t, ok, "a canceled key entry must not persist a partial credential")
	assert.False(t, state.IsEnabled("deepseek"))
}

func TestRunSetupUnknownProviderChoiceErrors(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{
		selectPick: func(call int, options []SetupOption) SetupOption {
			return SetupOption{Label: "bogus", Value: "not-a-real-provider"}
		},
	}
	_, err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.Error(t, err)
}
