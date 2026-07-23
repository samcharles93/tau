package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/updater"
	urfavecli "github.com/urfave/cli/v3"
)

func updateCmd(currentVersion string) *urfavecli.Command {
	return &urfavecli.Command{
		Name:        "update",
		Usage:       "Update tau from GitHub releases; set updates.mode=auto for background checks (reserved)",
		Description: "By default update checks only run when you invoke `tau update`.\nSet `updates.mode: auto` in config to enable automatic background update checks (reserved; currently behaves as `warn`).\nSet `updates.mode: disabled` to disable all update checks (including this command).\nDev builds are excluded from update checks.",
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{
				Name:  "check",
				Usage: "Check for an available update without installing it",
			},
			&urfavecli.StringFlag{
				Name:  "version",
				Usage: "Release tag to install (defaults to latest)",
			},
			&urfavecli.StringFlag{
				Name:  "repo",
				Usage: "GitHub repository to update from",
				Value: updater.DefaultRepo,
			},
			&urfavecli.BoolFlag{
				Name:  "force",
				Usage: "Reinstall even when the selected version is already installed",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			cfg, _ := config.LoadConfigAllowEmpty()
			mode := cfg.Updates.Mode
			if mode == "" {
				mode = "warn"
			}
			if mode == "auto" {
				slog.Warn("updates.mode 'auto' is reserved; behaving as 'warn'")
				mode = "warn"
			}
			if mode == "disabled" {
				fmt.Fprintf(cmd.Root().Writer, "update checks are disabled (updates.mode: disabled)\n")
				return nil
			}

			result, err := updater.Run(ctx, updater.Options{
				CurrentVersion: currentVersion,
				TargetVersion:  cmd.String("version"),
				Repo:           cmd.String("repo"),
				CheckOnly:      cmd.Bool("check"),
				Force:          cmd.Bool("force"),
			})
			if errors.Is(err, updater.ErrNoUpdate) {
				fmt.Fprintf(cmd.Root().Writer, "tau is already up to date (%s)\n", result.CurrentVersion)
				return nil
			}
			if errors.Is(err, updater.ErrDevBuild) {
				fmt.Fprintf(cmd.Root().Writer, "dev build detected — update checks only apply to release builds\n")
				return nil
			}
			if err != nil {
				return err
			}

			if cmd.Bool("check") {
				fmt.Fprintf(
					cmd.Root().Writer,
					"update available: %s -> %s\n%s\n",
					result.CurrentVersion,
					result.TargetVersion,
					result.ReleaseURL,
				)
				return nil
			}

			fmt.Fprintf(
				cmd.Root().Writer,
				"updated tau: %s -> %s\n",
				result.CurrentVersion,
				result.TargetVersion,
			)
			return nil
		},
	}
}
