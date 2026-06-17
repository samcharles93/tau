package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	tauconfig "github.com/samcharles93/tau/internal/config"
	taulogger "github.com/samcharles93/tau/internal/logger"
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
	taulogger.SetDefault(logFile, taulogger.Options{
		Level:  level,
		Format: taulogger.FormatText,
	})
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
				Name:    "prompt",
				Aliases: []string{"p"},
				Usage:   "Single-shot mode: process prompt and exit",
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

			cfg, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}
			initLogging(cmd.Bool("verbose") || cfg.Debug)
			opts := chatOptionsFromCmd(cmd, cfg, selectedProvider, version)
			prompt := cmd.String("prompt")
			if prompt != "" {
				if args := cmd.Args(); args.Len() > 0 {
					prompt = strings.TrimSpace(prompt + " " + strings.Join(args.Slice(), " "))
				}
				return app.RunStdIn(ctx, opts, prompt)
			}
			return app.RunChat(ctx, opts)
		},
	}
}

func splitProviderModel(raw string) (providerPart string, modelPart string, ok bool) {
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

func chatOptionsFromCmd(cmd *urfavecli.Command, cfg tauconfig.Config, provider tauconfig.ProviderConfig, version string) app.ChatOptions {
	return app.ChatOptions{
		Config:          cfg,
		Provider:        provider,
		Insecure:        cmd.Bool("insecure"),
		Model:           cmd.String("model"),
		MaxTokens:       cmd.Int("max-tokens"),
		Temperature:     cmd.Float("temperature"),
		Version:         version,
		ResumeSessionID: cmd.String("resume"),
	}
}
