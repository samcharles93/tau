package cli

import (
	"context"
	"fmt"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/maas"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
	urfavecli "github.com/urfave/cli/v3"
)

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
			insecure := cmd.Root().Bool("insecure")

			maasToken, err := maas.ResolveMaaSToken(ctx, endpoint, insecure, "", cmd.String("ocp-token"), cmd.String("expiry"))
			if err != nil {
				return err
			}

			fmt.Print(maasToken)
			return nil
		},
	}
}
