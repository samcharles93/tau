package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/samcharles93/tau/internal/config"
)

// configCmd groups configuration inspection subcommands.
func configCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "config",
		Usage: "Inspect and validate the tau configuration",
		Commands: []*urfavecli.Command{
			configCheckCmd(),
		},
	}
}

// configCheckCmd implements `tau config check`: it loads the merged global +
// project config WITHOUT failing on the first validation error
// (LoadConfigUnvalidated) and prints every issue CheckAll finds - errors and
// warnings - so a hand-edited config.yaml shows all its problems at once
// instead of one per restart. Exits non-zero when any error-severity issue
// is found, so it can gate scripts.
func configCheckCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:    "check",
		Aliases: []string{"validate"},
		Usage:   "Validate the loaded config and print ALL issues (not just the first)",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			cfg, err := config.LoadConfigUnvalidated()
			if err != nil {
				return err
			}
			return runConfigCheck(cfg, os.Stdout)
		},
	}
}

// runConfigCheck prints the config report to w and returns an exit error when
// the config has error-severity issues. Split from the urfave Action so tests
// can drive it with a constructed config and a buffer.
func runConfigCheck(cfg config.Config, w io.Writer) error {
	issues := config.CheckAll(cfg)
	fmt.Fprint(w, formatConfigReport(issues))
	errCount := 0
	for _, issue := range issues {
		if issue.IsError() {
			errCount++
		}
	}
	if errCount > 0 {
		return urfavecli.Exit(fmt.Sprintf("%d config error(s) found", errCount), 1)
	}
	return nil
}

// formatConfigReport renders the CheckAll issue list as a human-readable
// report: a summary line followed by one indented line per issue.
func formatConfigReport(issues []config.ConfigIssue) string {
	if len(issues) == 0 {
		return "Config check: no issues found.\n"
	}
	errCount, warnCount := 0, 0
	for _, issue := range issues {
		if issue.IsError() {
			errCount++
		} else {
			warnCount++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Config check: %d issue(s) - %d error(s), %d warning(s)\n", len(issues), errCount, warnCount)
	for _, issue := range issues {
		fmt.Fprintf(&out, "  [%s] %s\n", issue.Severity, issue.Message)
	}
	return out.String()
}
