package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/app"
	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
	urfavecli "github.com/urfave/cli/v3"
)

var defaultChatParameters = aimchat.DefaultParameters()

func chatCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "chat",
		Usage: "Stream a prompt against a MaaS chat model",
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
			&urfavecli.StringFlag{
				Name:  "ocp-token",
				Usage: "Skip OAuth and use a pre-existing OCP token",
			},
			&urfavecli.StringFlag{
				Name:  "maas-token",
				Usage: "Skip OAuth+exchange and use a pre-existing MaaS token",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			endpoint, err := platform.ResolveEndpoint(cmd.Root().String("endpoint"))
			if err != nil {
				return err
			}

			return app.RunChat(ctx, app.ChatOptions{
				Endpoint:     endpoint,
				Insecure:     cmd.Root().Bool("insecure"),
				MaaSToken:    cmd.String("maas-token"),
				OCPToken:     cmd.String("ocp-token"),
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
		Usage: "Print a fresh MaaS JWT to stdout (pipe-friendly)",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:  "ocp-token",
				Usage: "Skip OAuth and exchange a pre-existing OCP token",
			},
			&urfavecli.StringFlag{
				Name:  "expiry",
				Usage: "MaaS token lifetime (e.g. 4h, 24h)",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			endpoint, err := platform.ResolveEndpoint(cmd.Root().String("endpoint"))
			if err != nil {
				return err
			}

			token, err := app.MintToken(ctx, app.TokenOptions{
				Endpoint: endpoint,
				Insecure: cmd.Root().Bool("insecure"),
				OCPToken: cmd.String("ocp-token"),
				Expiry:   cmd.String("expiry"),
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
		Usage: "List available models on the MaaS platform",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:  "ocp-token",
				Usage: "Skip OAuth and use a pre-existing OCP token",
			},
			&urfavecli.StringFlag{
				Name:  "maas-token",
				Usage: "Skip OAuth+exchange and use a pre-existing MaaS token",
			},
			&urfavecli.BoolFlag{
				Name:  "json",
				Usage: "Output as JSON instead of a table",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			endpoint, err := platform.ResolveEndpoint(cmd.Root().String("endpoint"))
			if err != nil {
				return err
			}

			models, err := app.DiscoverModels(ctx, app.ModelsOptions{
				Endpoint:  endpoint,
				Insecure:  cmd.Root().Bool("insecure"),
				MaaSToken: cmd.String("maas-token"),
				OCPToken:  cmd.String("ocp-token"),
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
