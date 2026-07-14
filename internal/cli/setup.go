package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/samcharles93/tau/internal/app"
	urfavecli "github.com/urfave/cli/v3"
)

// cliSetupPrompter implements app.SetupPrompter using this package's
// terminal Select/ReadSecret primitives (see prompt.go), translating this
// package's cancellation sentinel into app's so RunSetup stays free of any
// terminal dependency.
type cliSetupPrompter struct{}

func (cliSetupPrompter) Select(ctx context.Context, title string, options []app.SetupOption) (app.SetupOption, error) {
	opts := make([]SelectOption, len(options))
	for i, o := range options {
		opts[i] = SelectOption{Label: o.Label, Value: o.Value}
	}
	choice, err := Select(ctx, os.Stdout, os.Stdin, title, opts)
	if err != nil {
		if errors.Is(err, ErrPromptCanceled) {
			return app.SetupOption{}, app.ErrSetupCanceled
		}
		return app.SetupOption{}, err
	}
	return app.SetupOption{Label: choice.Label, Value: choice.Value}, nil
}

func (cliSetupPrompter) ReadSecret(ctx context.Context, prompt string) (string, error) {
	secret, err := ReadSecret(ctx, os.Stdout, int(os.Stdin.Fd()), prompt)
	if err != nil {
		if errors.Is(err, ErrPromptCanceled) {
			return "", app.ErrSetupCanceled
		}
		return "", err
	}
	return secret, nil
}

// setupCmd is `tau setup`: the interactive first-run wizard. It owns its own
// exit codes (130 on cancel, 1 on failure) rather than returning an error to
// the root command's generic handler, which always exits 1 and would print a
// second, redundant "error: ..." line on top of the one printed here.
func setupCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "setup",
		Usage: "Interactive wizard: pick a provider, authenticate, and choose a default model",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			result, err := app.RunSetup(ctx, app.RunSetupOptions{
				Prompter: cliSetupPrompter{},
				Stdout:   os.Stdout,
				Insecure: cmd.Root().Bool("insecure"),
			})
			code, stdoutMsg, stderrMsg := setupExitPlan(result, err)
			if stdoutMsg != "" {
				fmt.Println(stdoutMsg)
			}
			if stderrMsg != "" {
				fmt.Fprintln(os.Stderr, stderrMsg)
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

// setupExitPlan decides the exit code and the (stdout, stderr) messages for
// a RunSetup outcome. Kept pure and separate from setupCmd's Action so it's
// testable without invoking os.Exit.
func setupExitPlan(result app.SetupResult, err error) (exitCode int, stdoutMsg string, stderrMsg string) {
	switch {
	case err == nil:
		return 0, setupSuccessMessage(result), ""
	case errors.Is(err, app.ErrSetupCanceled):
		return 130, "", "setup canceled"
	default:
		return 1, "", fmt.Sprintf("setup failed: %v", err)
	}
}

func setupSuccessMessage(result app.SetupResult) string {
	if result.Model != "" {
		return fmt.Sprintf("%s\nRun 'tau' to start chatting with %s on %s.",
			styleBold("Setup complete."), result.Model, result.ProviderName)
	}
	return fmt.Sprintf("%s\n%s is configured, but no default model was selected. Run 'tau' and use /model to pick one, or 'tau setup' again anytime.",
		styleBold("Setup complete."), result.ProviderName)
}
