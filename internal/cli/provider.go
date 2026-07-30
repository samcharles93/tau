package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/samcharles93/tau/internal/app"
)

var errProviderUsage = errors.New("provider command usage error")

type providerUsageError struct {
	message string
}

func (e providerUsageError) Error() string { return e.message }
func (e providerUsageError) Unwrap() error { return errProviderUsage }

func providerUsage(message string) error {
	return providerUsageError{message: message}
}

func providerCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "provider",
		Usage: "List, authenticate, and log out providers",
		Commands: []*urfavecli.Command{
			{
				Name:  "list",
				Usage: "List providers and their current authentication state",
				Action: func(_ context.Context, _ *urfavecli.Command) error {
					statuses, err := app.ListProviders()
					if err == nil {
						renderProviderList(os.Stdout, statuses)
					}
					finishProviderCommand("provider list", err)
					return nil
				},
			},
			{
				Name:      "login",
				Usage:     "Authenticate an OAuth or API-key provider",
				ArgsUsage: "[name]",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{
						Name:  "enterprise-domain",
						Usage: "GitHub Enterprise domain for github-copilot",
					},
				},
				Action: func(ctx context.Context, cmd *urfavecli.Command) error {
					if cmd.Args().Len() > 1 {
						finishProviderCommand("provider login", providerUsage("usage: tau provider login [name]"))
						return nil
					}
					name, err := providerLoginName(ctx, cmd.Args().First(), term.IsTerminal(int(os.Stdin.Fd())), os.Stdout, os.Stdin, Select)
					if err == nil {
						var result app.ProviderLoginResult
						result, err = app.RunProviderLogin(ctx, app.ProviderLoginOptions{
							ProviderID:       name,
							EnterpriseDomain: cmd.String("enterprise-domain"),
							Prompter:         cliSetupPrompter{},
							Stdout:           os.Stdout,
							Insecure:         cmd.Root().Bool("insecure"),
						})
						if err == nil {
							_, _ = fmt.Fprintf(os.Stdout, "%s login complete.\n", result.ProviderName)
						}
					}
					finishProviderCommand("provider login", err)
					return nil
				},
			},
			{
				Name:      "logout",
				Usage:     "Disable a provider and clear its managed credentials",
				ArgsUsage: "<name>",
				Action: func(_ context.Context, cmd *urfavecli.Command) error {
					if cmd.Args().Len() != 1 {
						finishProviderCommand("provider logout", providerUsage("usage: tau provider logout <name>"))
						return nil
					}
					displayName, err := app.LogoutProvider(cmd.Args().First())
					if err == nil {
						_, _ = fmt.Fprintf(os.Stdout, "%s logged out.\n", displayName)
					}
					finishProviderCommand("provider logout", err)
					return nil
				},
			},
		},
	}
}

type providerSelectFunc func(context.Context, io.Writer, io.Reader, string, []SelectOption) (SelectOption, error)

func providerLoginName(ctx context.Context, name string, interactive bool, stdout io.Writer, stdin io.Reader, selectProvider providerSelectFunc) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" {
		return name, nil
	}
	if !interactive {
		return "", providerUsage("provider name is required when stdin is not a terminal; usage: tau provider login <name>")
	}
	choices := app.ProviderLoginChoices()
	options := make([]SelectOption, 0, len(choices))
	for _, choice := range choices {
		options = append(options, SelectOption{Label: choice.Label, Value: choice.ID})
	}
	selected, err := selectProvider(ctx, stdout, stdin, "Select a provider", options)
	if err != nil {
		return "", err
	}
	return selected.Value, nil
}

func renderProviderList(w io.Writer, statuses []app.ProviderStatus) {
	providerWidth, statusWidth, sourceWidth, authWidth := len("PROVIDER"), len("STATUS"), len("SOURCE"), len("AUTH")
	for _, status := range statuses {
		providerWidth = max(providerWidth, len(status.ID))
		statusWidth = max(statusWidth, len(status.Status))
		sourceWidth = max(sourceWidth, len(status.Source))
		authWidth = max(authWidth, len(status.Auth))
	}

	_, _ = fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  %s\n", providerWidth, "PROVIDER", statusWidth, "STATUS", sourceWidth, "SOURCE", authWidth, "AUTH", "DETAILS")
	for _, status := range statuses {
		_, _ = fmt.Fprintf(w, "%-*s  %-*s  %-*s  %-*s  %s\n", providerWidth, status.ID, statusWidth, status.Status, sourceWidth, status.Source, authWidth, status.Auth, status.Details)
	}
}

func providerExitPlan(action string, err error) (exitCode int, stderrMsg string) {
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, errProviderUsage):
		return 2, err.Error()
	case errors.Is(err, ErrPromptCanceled), errors.Is(err, context.Canceled), errors.Is(err, app.ErrSetupCanceled):
		return 130, action + " canceled"
	default:
		return 1, fmt.Sprintf("%s failed: %v", action, err)
	}
}

func finishProviderCommand(action string, err error) {
	code, stderrMsg := providerExitPlan(action, err)
	if stderrMsg != "" {
		_, _ = fmt.Fprintln(os.Stderr, stderrMsg)
	}
	if code != 0 {
		os.Exit(code)
	}
}
