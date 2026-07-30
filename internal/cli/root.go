package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/samcharles93/tau/internal/app"
	tauconfig "github.com/samcharles93/tau/internal/config"
	taulogger "github.com/samcharles93/tau/internal/logger"
)

func initLogging(debug bool, version string) {
	logPath := filepath.Join(tauconfig.Dir(), "tau.log")

	// Rotate the log if it has grown too large (10 MiB). Keep one old copy.
	const maxSize = 10 * 1024 * 1024
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxSize {
		rotated := logPath + ".1"
		_ = os.Remove(rotated)
		_ = os.Rename(logPath, rotated)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	// TAU_LOG_LEVEL overrides the default (Info) but --verbose / config.debug
	// still wins to ensure explicit user intent is honoured.
	if envLevel := os.Getenv("TAU_LOG_LEVEL"); envLevel != "" && !debug {
		switch strings.ToLower(strings.TrimSpace(envLevel)) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	taulogger.SetDefault(logFile, taulogger.Options{
		Level:  level,
		Format: taulogger.FormatText,
	})

	// Write a run separator so each launch is visually distinct in the log.
	slog.Info("── tau start ──", "version", version, "pid", os.Getpid())
}

func NewRootCommand(version string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:    "tau",
		Usage:   "Provider-agnostic OpenAI-compatible chat client",
		Version: version,
		Commands: []*urfavecli.Command{
			setupCmd(),
			providerCmd(),
			tokenCmd(),
			modelsCmd(),
			refreshCmd(),
			sessionsCmd(),
			skillsCmd(),
			pluginsCmd(),
			updateCmd(version),
			workspaceCodesearchCmd(),
		},
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:    "provider",
				Usage:   "Configured provider name",
				Sources: urfavecli.EnvVars("TAU_PROVIDER"),
			},
			&urfavecli.BoolFlag{
				Name:    "insecure",
				Usage:   "Skip TLS certificate verification",
				Sources: urfavecli.EnvVars("TAU_INSECURE"),
			},
			&urfavecli.BoolFlag{
				Name:    "verbose",
				Usage:   "Show progress/debug messages on stderr",
				Sources: urfavecli.EnvVars("TAU_VERBOSE"),
			},
			&urfavecli.StringFlag{
				Name:  "model",
				Usage: "Model ID to use for chat (supports provider:model, e.g. openrouter:nvidia/nemotron-3-ultra)",
			},
			&urfavecli.IntFlag{
				Name:  "max-tokens",
				Usage: "Maximum completion tokens per response (0 = provider/model default)",
				Value: defaultChatParameters.MaxTokens,
			},
			&urfavecli.FloatFlag{
				Name:  "temperature",
				Usage: "Sampling temperature for the model response",
				Value: defaultChatParameters.Temperature,
			},
			&urfavecli.StringFlag{
				Name:    "resume",
				Aliases: []string{"r"},
				Usage:   "Resume a saved session (pass ID or 'latest' for most recent)",
			},
			&urfavecli.BoolFlag{
				Name:    "execute",
				Aliases: []string{"x"},
				Usage:   "Execute mode: process prompt and exit (reads from stdin when piped, positional args when provided)",
			},
			&urfavecli.StringFlag{
				Name:    "prompt",
				Aliases: []string{"p"},
				Usage:   "Deprecated: use -x/--execute instead. One-shot mode: process prompt and exit",
			},
			&urfavecli.BoolFlag{
				Name:  "web",
				Usage: "Start the web UI and open the browser",
			},
			&urfavecli.IntFlag{
				Name:  "port",
				Usage: "HTTP port for the web UI (0 = auto-assign)",
				Value: 0,
			},
			&urfavecli.BoolFlag{
				Name:    "legacy-tui",
				Aliases: []string{"legacy"},
				Usage:   "Use the legacy inline TUI instead of the default Bubbletea-based TUI",
			},
			&urfavecli.BoolFlag{
				Name:  "no-web",
				Usage: "Do not start the web UI",
			},
			&urfavecli.StringSliceFlag{
				Name:  "skill-dir",
				Usage: "Additional skill directories (repeatable)",
			},
			&urfavecli.StringFlag{
				Name:  "agent",
				Usage: "Agent spec to use as root identity (default: tau)",
			},
			&urfavecli.BoolFlag{
				Name:    "trust-project-root-spec",
				Usage:   "Trust a project-level tau.agent.md root-spec override for this run, bypassing the confirmation prompt",
				Sources: urfavecli.EnvVars("TAU_TRUST_PROJECT_ROOT_SPEC"),
			},
			&urfavecli.BoolFlag{
				Name:   "child",
				Usage:  "Run as a headless agent child process (internal use)",
				Hidden: true,
			},
			&urfavecli.StringSliceFlag{
				Name:    "tools",
				Aliases: []string{"t"},
				Usage:   "Restrict visible tools (comma-separated, repeatable; empty = all tools)",
				Sources: urfavecli.EnvVars("TAU_TOOLS"),
			},
			&urfavecli.BoolFlag{
				Name:    "ephemeral",
				Usage:   "Do not persist this session to the session store",
				Sources: urfavecli.EnvVars("TAU_EPHEMERAL"),
			},
			&urfavecli.BoolFlag{
				Name:    "jsonl",
				Aliases: []string{"stream-json"},
				Usage:   "Output framed JSONL events on stdout instead of plain text (execute mode only)",
			},
			&urfavecli.BoolFlag{
				Name:  "skip-setup",
				Usage: "Do not auto-launch the setup wizard when no provider is configured",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			// Support explicit provider:model syntax (preferred), e.g.
			// --model openrouter:nvidia/nemotron-3-ultra.
			// Also support legacy provider/model syntax for compatibility.
			if cmd.String("provider") == "" {
				if providerPart, modelPart, ok := splitProviderModel(cmd.String("model")); ok {
					if providerPart != "" && modelPart != "" {
						_ = cmd.Set("provider", providerPart)
						_ = cmd.Set("model", modelPart)
					}
				}
			}

			cfg, selectedProvider, err := loadProvider(ctx, cmd)
			if err != nil {
				switch {
				case shouldAutoSetup(err, cmd):
					cfg, selectedProvider, err = autoInvokeSetup(ctx, cmd)
					if err != nil {
						if errors.Is(err, app.ErrSetupCanceled) {
							fmt.Fprintln(os.Stderr, "setup canceled")
							os.Exit(130)
						}
						return err
					}
				case shouldSkipSetupContinue(err, cmd):
					// Fall through into RunChat/RunStdIn with zero providers:
					// its existing "No models available - use /provider to
					// enable a provider" startup guidance (run.go) takes over
					// instead of hard-failing before the user ever sees the app.
				default:
					return err
				}
			}
			initLogging(cmd.Bool("verbose") || cfg.Debug, version)
			opts := chatOptionsFromCmd(cmd, cfg, selectedProvider, version)
			if cmd.Bool("child") {
				err := app.RunChild(ctx, opts)
				app.ExitChild(err)
				// unreachable - exitChild calls os.Exit
				return err
			}
			prompt, err := resolveExecutePrompt(cmd)
			if err != nil {
				return err
			}
			if prompt != "" {
				// Execute mode never starts the web server.
				opts.Web = false
				opts.NoWeb = true
				return app.RunStdIn(ctx, opts, prompt)
			}
			return app.RunChat(ctx, opts)
		},
	}
}

// resolveExecutePrompt resolves the prompt for execute mode from --execute
// (positional args or stdin) or --prompt (deprecated flag value).
// --execute takes precedence.
func resolveExecutePrompt(cmd *urfavecli.Command) (string, error) {
	if cmd.Bool("execute") {
		// Collect prompt from positional args.
		if args := cmd.Args(); args.Len() > 0 {
			return strings.TrimSpace(strings.Join(args.Slice(), " ")), nil
		}
		// No args; check for piped stdin.
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("reading stdin: %w", err)
			}
			return strings.TrimSpace(string(data)), nil
		}
		return "", urfavecli.Exit("execute mode requires a prompt argument or piped stdin\nusage: tau -x <prompt>  or  echo <prompt> | tau -x", 2)
	}
	// Fallback to deprecated --prompt.
	if prompt := cmd.String("prompt"); prompt != "" {
		if args := cmd.Args(); args.Len() > 0 {
			prompt = strings.TrimSpace(prompt + " " + strings.Join(args.Slice(), " "))
		}
		return prompt, nil
	}
	return "", nil
}

// isOneShot reports whether the command is in one-shot/execute mode
// (either via --execute or the deprecated --prompt flag).
func isOneShot(cmd *urfavecli.Command) bool {
	return cmd.Bool("execute") || cmd.String("prompt") != ""
}

// shouldAutoSetup reports whether the root command should launch the setup
// wizard in place of failing outright. Only applies to the specific "zero
// usable providers" failure (an already-configured-but-unavailable provider
// should surface its own actionable error, not trigger a wizard the user
// didn't ask for), and only when a human is actually present to drive it:
// not suppressed via --skip-setup, not a headless child process, not
// one-shot --prompt or --execute mode (which may be scripted even with a
// TTY attached), and stdin is a real terminal (so a piped invocation like
// `echo hi | tau` gets the plain actionable error instead of hanging on a
// prompt no one can answer).
func shouldAutoSetup(err error, cmd *urfavecli.Command) bool {
	if !errors.Is(err, errNoProviders) {
		return false
	}
	if cmd.Bool("skip-setup") || cmd.Bool("child") || isOneShot(cmd) {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// shouldSkipSetupContinue reports whether the root command should proceed
// into the interactive chat path anyway, with zero configured providers,
// rather than failing on the "no usable providers" error. Only applies when
// the user explicitly passed --skip-setup (opting into this on purpose) and
// only for the genuinely interactive path - a headless --child process or
// one-shot --prompt/--execute run has no coherent way to "continue with
// guidance" the way the interactive TUI's existing startup messaging does
// (run.go's RunChat), so those still get the plain actionable error.
func shouldSkipSetupContinue(err error, cmd *urfavecli.Command) bool {
	if !errors.Is(err, errNoProviders) {
		return false
	}
	if !cmd.Bool("skip-setup") {
		return false
	}
	return !cmd.Bool("child") && !isOneShot(cmd)
}

// autoInvokeSetup runs the interactive setup wizard, then re-resolves the
// provider the same way the normal startup path does, so a freshly
// configured default_provider/default_model (or an explicit --provider/
// --model flag) is picked up immediately without a second process launch.
func autoInvokeSetup(ctx context.Context, cmd *urfavecli.Command) (tauconfig.Config, tauconfig.ProviderConfig, error) {
	fmt.Println(styleBold("No usable provider is configured - let's set one up."))
	if _, err := app.RunSetup(ctx, app.RunSetupOptions{
		Prompter: cliSetupPrompter{},
		Stdout:   os.Stdout,
		Insecure: cmd.Bool("insecure"),
	}); err != nil {
		return tauconfig.Config{}, tauconfig.ProviderConfig{}, err
	}
	return loadProvider(ctx, cmd)
}

func splitProviderModel(raw string) (providerPart, modelPart string, ok bool) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return "", "", false
	}

	// Preferred syntax: provider:model
	if provider, nestedModel, found := strings.Cut(model, ":"); found {
		if provider == "" || nestedModel == "" {
			return "", "", false
		}
		return provider, nestedModel, true
	}

	// Backward compatibility: provider/model with nested paths preserved.
	if provider, nestedModel, found := strings.Cut(model, "/"); found {
		if provider == "" || nestedModel == "" {
			return "", "", false
		}
		return provider, nestedModel, true
	}

	return "", "", false
}

func splitToolsFlag(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	for _, s := range raw {
		for part := range strings.SplitSeq(s, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func chatOptionsFromCmd(cmd *urfavecli.Command, cfg tauconfig.Config, provider tauconfig.ProviderConfig, version string) app.ChatOptions {
	outputFormat := ""
	if cmd.Bool("jsonl") {
		outputFormat = "jsonl"
	}
	return app.ChatOptions{
		Config:               cfg,
		Provider:             provider,
		Insecure:             cmd.Bool("insecure"),
		Model:                cmd.String("model"),
		MaxTokens:            cmd.Int("max-tokens"),
		Temperature:          cmd.Float("temperature"),
		Version:              version,
		ResumeSessionID:      cmd.String("resume"),
		NewTUI:               !cmd.Bool("legacy-tui"),
		Web:                  cmd.Bool("web"),
		WebPort:              cmd.Int("port"),
		NoWeb:                cmd.Bool("no-web"),
		SkillDirs:            cmd.StringSlice("skill-dir"),
		Ephemeral:            cmd.Bool("ephemeral"),
		AllowedTools:         splitToolsFlag(cmd.StringSlice("tools")),
		AgentSpec:            firstNonEmptyString(cmd.String("agent"), "tau"),
		TrustProjectRootSpec: cmd.Bool("trust-project-root-spec"),
		OutputFormat:         outputFormat,
	}
}
