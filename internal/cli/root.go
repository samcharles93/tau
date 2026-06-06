package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	tauconfig "github.com/samcharles93/tau/internal/config"
	urfavecli "github.com/urfave/cli/v3"
)

func initLogging(debug bool) {
	logPath := filepath.Join(tauconfig.Dir(), "tau.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func NewRootCommand(version string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:    "tau",
		Usage:   "Provider-agnostic OpenAI-compatible chat client",
		Version: version,
		Commands: []*urfavecli.Command{
			tokenCmd(),
			modelsCmd(),
			sessionsCmd(),
		},
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:    "provider",
				Aliases: []string{"p"},
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
				Usage: "Model ID to use for chat",
			},
			&urfavecli.StringFlag{
				Name:  "system-prompt",
				Usage: "Override the system prompt for this chat session",
			},
			&urfavecli.IntFlag{
				Name:  "max-tokens",
				Usage: "Maximum completion tokens per response",
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
			&urfavecli.StringFlag{
				Name:  "prompt",
				Usage: "Single-shot mode: process prompt and exit",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			// Support provider/model syntax: --model openrouter/gpt-5.3
			if modelArg := cmd.String("model"); strings.Contains(modelArg, "/") {
				parts := strings.SplitN(modelArg, "/", 2)
				if parts[0] != "" && parts[1] != "" {
					_ = cmd.Set("provider", parts[0])
					_ = cmd.Set("model", parts[1])
				}
			}

			cfg, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}
			initLogging(cmd.Bool("verbose") || cfg.Debug)
			opts := chatOptionsFromCmd(cmd, cfg, selectedProvider, version)
			prompt := cmd.String("prompt")
			if prompt != "" {
				return app.RunSingleShot(ctx, opts, prompt)
			}
			return app.RunChat(ctx, opts)
		},
	}
}

func chatOptionsFromCmd(cmd *urfavecli.Command, cfg tauconfig.Config, provider tauconfig.ProviderConfig, version string) app.ChatOptions {
	return app.ChatOptions{
		Config:          cfg,
		Provider:        provider,
		Insecure:        cmd.Bool("insecure"),
		Model:           cmd.String("model"),
		SystemPrompt:    cmd.String("system-prompt"),
		MaxTokens:       cmd.Int("max-tokens"),
		Temperature:     cmd.Float("temperature"),
		Version:         version,
		ResumeSessionID: cmd.String("resume"),
	}
}
