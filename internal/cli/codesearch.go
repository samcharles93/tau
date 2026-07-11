package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/samcharles93/tau/internal/indexing"
	urfavecli "github.com/urfave/cli/v3"
)

func workspaceCodesearchCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:   "workspace-codesearch",
		Hidden: true,
		Commands: []*urfavecli.Command{
			{
				Name: "build",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "root", Required: true},
					&urfavecli.StringFlag{Name: "index", Required: true},
				},
				Action: func(ctx context.Context, cmd *urfavecli.Command) error {
					if err := indexing.BuildIndex(ctx, cmd.String("root"), cmd.String("index")); err != nil {
						return fmt.Errorf("build workspace index: %w", err)
					}
					return nil
				},
			},
			{
				Name: "candidates",
				Flags: []urfavecli.Flag{
					&urfavecli.StringFlag{Name: "index", Required: true},
					&urfavecli.StringFlag{Name: "pattern", Required: true},
					&urfavecli.BoolFlag{Name: "literal"},
					&urfavecli.BoolFlag{Name: "case-sensitive"},
				},
				Action: func(_ context.Context, cmd *urfavecli.Command) error {
					files, err := indexing.IndexCandidates(cmd.String("index"), cmd.String("pattern"), cmd.Bool("literal"), cmd.Bool("case-sensitive"))
					if err != nil {
						return fmt.Errorf("query workspace index: %w", err)
					}
					return json.NewEncoder(os.Stdout).Encode(files)
				},
			},
		},
	}
}
