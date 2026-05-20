package cli

import urfavecli "github.com/urfave/cli/v3"

func NewRootCommand(version string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:    "aim",
		Usage:   "AIM platform CLI — tokens, models, chat, benchmarking",
		Version: version,
		Commands: []*urfavecli.Command{
			tokenCmd(),
			modelsCmd(),
			chatCmd(),
			// bench will be added in M3
		},
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:    "endpoint",
				Aliases: []string{"e"},
				Usage:   "Endpoint as dc/env (e.g. rcc/npr, wsdc/edev)",
				Sources: urfavecli.EnvVars("AIM_ENDPOINT"),
			},
			&urfavecli.BoolFlag{
				Name:    "insecure",
				Usage:   "Skip TLS certificate verification",
				Sources: urfavecli.EnvVars("AIM_INSECURE"),
			},
			&urfavecli.BoolFlag{
				Name:    "verbose",
				Usage:   "Show progress/debug messages on stderr",
				Sources: urfavecli.EnvVars("AIM_VERBOSE"),
			},
		},
	}
}
