package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

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
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			cfg, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}

			initLogging(cmd.Bool("verbose") || cfg.Debug)

			resumeID := cmd.String("resume")

			return app.RunChat(ctx, app.ChatOptions{
				Config:          cfg,
				Provider:        selectedProvider,
				Insecure:        cmd.Bool("insecure"),
				Model:           cmd.String("model"),
				SystemPrompt:    cmd.String("system-prompt"),
				MaxTokens:       cmd.Int("max-tokens"),
				Temperature:     cmd.Float("temperature"),
				Version:         version,
				ResumeSessionID: resumeID,
			})
		},
	}
}
