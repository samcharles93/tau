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
	err := RunSetup(context.Background(), RunSetupOptions{})
	require.Error(t, err)
}

func TestRunSetupKeylessProviderPersists(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{selectPick: pickProviderThenFirst("ollama")}

	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)

	state, err := providers.LoadState()
	require.NoError(t, err)
	assert.True(t, state.IsEnabled("ollama"))

	cfg, err := tauconfig.LoadConfigAllowEmpty()
	require.NoError(t, err)
	assert.Equal(t, "ollama", cfg.DefaultProvider)
}

func TestRunSetupAPIKeyProviderPersists(t *testing.T) {
	sandboxConfigDir(t)
	prompter := &fakeSetupPrompter{
		selectPick:  pickProviderThenFirst("deepseek"),
		secretQueue: []string{"sk-test-key"},
	}

	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.NoError(t, err)

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
	prompter := &fakeSetupPrompter{
		selectPick:  pickProviderThenFirst("deepseek"),
		secretQueue: []string{"  ", "", "sk-test-key"},
	}

	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
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

	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
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

	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
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
	err := RunSetup(context.Background(), RunSetupOptions{Prompter: prompter})
	require.Error(t, err)
}
