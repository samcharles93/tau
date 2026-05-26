package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/samcharles93/tau/internal/app"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	urfavecli "github.com/urfave/cli/v3"
)

var defaultChatParameters = tauchat.DefaultParameters()

func chatCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "chat",
		Usage: "Stream a prompt against an OpenAI-compatible chat model",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:  "prompt",
				Usage: "Prompt text to send; if omitted, stdin is used when piped",
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
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			cfg, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}

			return app.RunChat(ctx, app.ChatOptions{
				Config:       cfg,
				Provider:     selectedProvider,
				Insecure:     cmd.Root().Bool("insecure"),
				Model:        cmd.String("model"),
				SystemPrompt: cmd.String("system-prompt"),
				MaxTokens:    cmd.Int("max-tokens"),
				Temperature:  cmd.Float("temperature"),
				Prompt:       cmd.String("prompt"),
			})
		},
	}
}

func tokenCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "token",
		Usage: "Print the resolved provider bearer token to stdout",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			_, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}

			token, err := app.ResolveToken(ctx, app.TokenOptions{
				Provider: selectedProvider,
				Insecure: cmd.Root().Bool("insecure"),
			})
			if err != nil {
				return err
			}

			fmt.Print(token)
			return nil
		},
	}
}

func modelsCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "models",
		Usage: "List available models from the configured provider",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{
				Name:  "json",
				Usage: "Output as JSON instead of a table",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			_, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}

			models, err := app.DiscoverModels(ctx, app.ModelsOptions{
				Provider: selectedProvider,
				Insecure: cmd.Root().Bool("insecure"),
			})
			if err != nil {
				return err
			}

			if cmd.Bool("json") {
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(models)
			}

			nameW := len("MODEL")
			for _, model := range models {
				if len(model.ID) > nameW {
					nameW = len(model.ID)
				}
			}

			fmt.Printf("%-*s  %-5s  %s\n", nameW, "MODEL", "READY", "URL")
			for _, model := range models {
				ready := "yes"
				if !model.Ready {
					ready = "no"
				}
				fmt.Printf("%-*s  %-5s  %s\n", nameW, model.ID, ready, model.URL)
			}
			return nil
		},
	}
}

func loadProvider(cmd *urfavecli.Command) (config.Config, config.ProviderConfig, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return config.Config{}, config.ProviderConfig{}, err
	}
	provider, err := config.ResolveProvider(cfg, cmd.Root().String("provider"))
	if err != nil {
		return config.Config{}, config.ProviderConfig{}, err
	}
	return cfg, provider, nil
}
