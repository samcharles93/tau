package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	urfavecli "github.com/urfave/cli/v3"
)

func TestSplitProviderModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		provider string
		model    string
		ok       bool
	}{
		{
			name:     "preferred colon syntax with nested model path",
			raw:      "openrouter:nvidia/nemotron-3-ultra",
			provider: "openrouter",
			model:    "nvidia/nemotron-3-ultra",
			ok:       true,
		},
		{
			name:     "legacy slash syntax remains supported",
			raw:      "openrouter/nvidia/nemotron-3-ultra",
			provider: "openrouter",
			model:    "nvidia/nemotron-3-ultra",
			ok:       true,
		},
		{
			name: "bare model is not split",
			raw:  "gpt-5.3",
			ok:   false,
		},
		{
			name: "empty is not split",
			raw:  "",
			ok:   false,
		},
		{
			name: "invalid colon format missing model",
			raw:  "openrouter:",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, model, ok := splitProviderModel(tc.raw)
			if ok != tc.ok {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.ok)
			}
			if provider != tc.provider {
				t.Fatalf("provider mismatch: got %q want %q", provider, tc.provider)
			}
			if model != tc.model {
				t.Fatalf("model mismatch: got %q want %q", model, tc.model)
			}
		})
	}
}

// buildTestCommand parses args against a command exposing the same
// skip-setup/child/prompt/execute flags root.go's Action reads, returning
// the resulting *urfavecli.Command for shouldAutoSetup/shouldSkipSetupContinue
// to inspect - mirrors how those functions actually receive their cmd
// argument in production, rather than hand-constructing flag state.
func buildTestCommand(t *testing.T, args []string) *urfavecli.Command {
	t.Helper()
	var captured *urfavecli.Command
	cmd := &urfavecli.Command{
		Name: "tau",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "skip-setup"},
			&urfavecli.BoolFlag{Name: "child"},
			&urfavecli.BoolFlag{Name: "execute", Aliases: []string{"x"}},
			&urfavecli.StringFlag{Name: "prompt", Aliases: []string{"p"}},
		},
		Action: func(ctx context.Context, c *urfavecli.Command) error {
			captured = c
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), args))
	return captured
}

func TestShouldAutoSetupRejectsWrongErrorType(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau"})
	assert.False(t, shouldAutoSetup(errors.New("some other failure"), cmd))
}

func TestShouldAutoSetupRejectsSkipSetup(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup"})
	assert.False(t, shouldAutoSetup(errNoProviders, cmd))
}

func TestShouldAutoSetupRejectsChildProcess(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--child"})
	assert.False(t, shouldAutoSetup(errNoProviders, cmd))
}

func TestShouldAutoSetupRejectsOneShotPrompt(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--prompt", "hi"})
	assert.False(t, shouldAutoSetup(errNoProviders, cmd))
}

func TestShouldAutoSetupRejectsOneShotExecute(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--execute"})
	assert.False(t, shouldAutoSetup(errNoProviders, cmd))
}

func TestShouldAutoSetupRejectsOneShotExecuteShort(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "-x"})
	assert.False(t, shouldAutoSetup(errNoProviders, cmd))
}

// The "returns true" case (no providers, no skip-setup/child/prompt/execute,
// TTY stdin) isn't exercised here: term.IsTerminal(stdin) is always false
// under `go test`, so this function can't distinguish that branch from the
// rejection cases above in a unit test. Verified manually instead via tmux
// (a real pty) - see TAU-126.

func TestShouldSkipSetupContinueRejectsWrongErrorType(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup"})
	assert.False(t, shouldSkipSetupContinue(errors.New("some other failure"), cmd))
}

func TestShouldSkipSetupContinueRequiresTheFlag(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau"})
	assert.False(t, shouldSkipSetupContinue(errNoProviders, cmd))
}

func TestShouldSkipSetupContinueAllowsPlainInteractive(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup"})
	assert.True(t, shouldSkipSetupContinue(errNoProviders, cmd))
}

func TestShouldSkipSetupContinueRejectsChildProcess(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup", "--child"})
	assert.False(t, shouldSkipSetupContinue(errNoProviders, cmd))
}

func TestShouldSkipSetupContinueRejectsOneShotPrompt(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup", "--prompt", "hi"})
	assert.False(t, shouldSkipSetupContinue(errNoProviders, cmd))
}

func TestShouldSkipSetupContinueRejectsOneShotExecute(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--skip-setup", "--execute"})
	assert.False(t, shouldSkipSetupContinue(errNoProviders, cmd))
}

func TestIsOneShot_PromptOnly(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--prompt", "hi"})
	assert.True(t, isOneShot(cmd))
}

func TestIsOneShot_ExecuteOnly(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "-x"})
	assert.True(t, isOneShot(cmd))
}

func TestIsOneShot_Neither(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau"})
	assert.False(t, isOneShot(cmd))
}

func TestExecuteFlagParsing_ShortFlag(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "-x"})
	assert.True(t, cmd.Bool("execute"))
}

func TestExecuteFlagParsing_LongFlag(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--execute"})
	assert.True(t, cmd.Bool("execute"))
}

func TestExecuteFlagParsing_ArgsAfterFlag(t *testing.T) {
	// Arguments after -x become positional args, not flag values.
	cmd := buildTestCommand(t, []string{"tau", "-x", "hello", "world"})
	assert.True(t, cmd.Bool("execute"))
	assert.Equal(t, []string{"hello", "world"}, cmd.Args().Slice())
}

func TestResolveExecutePrompt_FromArgs(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "-x", "hello", "world"})
	prompt, err := resolveExecutePrompt(cmd)
	require.NoError(t, err)
	assert.Equal(t, "hello world", prompt)
}

func TestResolveExecutePrompt_FallsBackToPrompt(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "--prompt", "from-prompt"})
	prompt, err := resolveExecutePrompt(cmd)
	require.NoError(t, err)
	assert.Equal(t, "from-prompt", prompt)
}

func TestResolveExecutePrompt_PrefersExecuteOverPrompt(t *testing.T) {
	cmd := buildTestCommand(t, []string{"tau", "-x", "from-execute", "--prompt", "from-prompt"})
	prompt, err := resolveExecutePrompt(cmd)
	require.NoError(t, err)
	assert.Equal(t, "from-execute", prompt)
}
