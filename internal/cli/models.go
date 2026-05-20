package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/maas"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
	urfavecli "github.com/urfave/cli/v3"
)

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
			insecure := cmd.Root().Bool("insecure")

			maasToken, err := maas.ResolveMaaSToken(ctx, endpoint, insecure, cmd.String("maas-token"), cmd.String("ocp-token"), "")
			if err != nil {
				return err
			}

			models, err := maas.DiscoverModels(ctx, endpoint, maasToken, insecure)
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
