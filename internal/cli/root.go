package cli

import urfavecli "github.com/urfave/cli/v3"

func NewRootCommand(version string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:    "tau",
		Usage:   "Provider-agnostic OpenAI-compatible chat client",
		Version: version,
		Commands: []*urfavecli.Command{
			tokenCmd(),
			modelsCmd(),
			chatCmd(),
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
		},
	}
}
