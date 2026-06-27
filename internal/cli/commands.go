package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/samcharles93/tau/internal/app"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
	urfavecli "github.com/urfave/cli/v3"
)

var defaultChatParameters = tauchat.DefaultParameters()

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

			fmt.Printf("%-*s  %s\n", nameW, "MODEL", "URL")
			for _, model := range models {
				fmt.Printf("%-*s  %s\n", nameW, model.ID, model.URL)
			}
			return nil
		},
	}
}

func refreshCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "refresh",
		Usage: "Refresh the models.dev catalog and list available models",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			_, selectedProvider, err := loadProvider(cmd)
			if err != nil {
				return err
			}

			models, err := app.RefreshModels(ctx, app.ModelsOptions{
				Provider: selectedProvider,
				Insecure: cmd.Root().Bool("insecure"),
			})
			if err != nil {
				return err
			}

			nameW := len("MODEL")
			for _, model := range models {
				if len(model.ID) > nameW {
					nameW = len(model.ID)
				}
			}

			fmt.Printf("%-*s  %s\n", nameW, "MODEL", "URL")
			for _, model := range models {
				fmt.Printf("%-*s  %s\n", nameW, model.ID, model.URL)
			}
			return nil
		},
	}
}

func sessionsCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "sessions",
		Usage: "Manage saved chat sessions",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			return listSessions(ctx)
		},
	}
}

func listSessions(ctx context.Context) error {
	s, err := openSessionStore()
	if err != nil {
		return err
	}
	defer s.Close()

	// Fetch up to 50 sessions for CLI listing.
	summaries, _, err := s.List(ctx, 50, "")
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(summaries) == 0 {
		fmt.Println("No saved sessions.")
		return nil
	}

	fmt.Printf("%-38s  %-20s  %4s  %10s  %8s  %s\n",
		"ID", "MODEL", "MSGS", "TOKENS", "COST", "DATE")
	for _, s := range summaries {
		fmt.Printf("%-38s  %-20s  %4d  %10s  $%6s  %s\n",
			s.ID,
			truncateString(s.ModelID, 20),
			s.MessageCount,
			formatTokensCLI(s.TotalTokens),
			formatCostCLI(s.Cost),
			s.CreatedAt.Local().Format("2006-01-02 15:04"),
		)
	}
	return nil
}

func openSessionStore() (store.SessionStore, error) {
	return sessions.OpenStore()
}

func formatTokensCLI(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatCostCLI(c float64) string {
	if c == 0 {
		return "0.00"
	}
	if c < 0.01 {
		return fmt.Sprintf("%.4f", c)
	}
	return fmt.Sprintf("%.2f", c)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func loadProvider(cmd *urfavecli.Command) (config.Config, config.ProviderConfig, error) {
	cfg, _, err := providers.Effective()
	if err != nil {
		return config.Config{}, config.ProviderConfig{}, err
	}
	if len(cfg.Providers) == 0 {
		return config.Config{}, config.ProviderConfig{}, noProvidersError()
	}
	provider, err := config.ResolveProvider(cfg, cmd.Root().String("provider"))
	if err != nil {
		return config.Config{}, config.ProviderConfig{}, err
	}
	return cfg, provider, nil
}

// noProvidersError explains how to get providers when none are usable, covering
// both auto-detected env keys and the interactive /login flow.
func noProvidersError() error {
	return fmt.Errorf("no usable providers: set a provider API key (e.g. OPENROUTER_API_KEY), "+
		"run tau and use /login to enable a provider, or declare one in %s", config.GlobalPath())
}

func skillsCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "skills",
		Usage: "List and inspect available skills",
		Commands: []*urfavecli.Command{
			skillsListCmd(),
			skillsInfoCmd(),
		},
	}
}

func skillsListCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "list",
		Usage: "List all discovered skills",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			cfg, err := config.LoadConfigAllowEmpty()
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()

			sources := skills.DefaultSources(cwd)
			sources = append(sources, skills.AdditionalSources(cfg.SkillPaths, skills.ScopeUser)...)
			sources = append(sources, skills.AdditionalSources(cmd.Root().StringSlice("skill-dir"), skills.ScopeUser)...)

			allSkills, diagnostics := skills.Discover(sources)

			// Build disabled lookup so status can be shown even for disabled skills.
			disabledSet := make(map[string]bool)
			for _, name := range cfg.DisabledSkills {
				disabledSet[strings.ToLower(strings.TrimSpace(name))] = true
			}

			if len(allSkills) == 0 {
				fmt.Println("No skills found.")
				return nil
			}

			fmt.Printf("%-38s  %-58s  %-8s  %-10s  %s\n",
				"NAME", "DESCRIPTION", "SCOPE", "INVOCABLE", "STATUS")
			for _, skill := range allSkills {
				status := "active"
				if disabledSet[strings.ToLower(skill.Name)] {
					status = "disabled"
				}

				userInv := "yes"
				if !skill.UserInvocable {
					userInv = "no"
				}

				fmt.Printf("%-38s  %-58s  %-8s  %-10s  %s\n",
					skill.Name,
					truncateString(skill.Description, 58),
					skill.Scope,
					userInv,
					status,
				)
			}

			if len(diagnostics) > 0 {
				fmt.Fprintf(os.Stderr, "\n%d diagnostic(s):\n", len(diagnostics))
				for _, d := range diagnostics {
					fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", d.Severity, d.Path, d.Message)
				}
			}

			return nil
		},
	}
}

func skillsInfoCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "info",
		Usage: "Show detailed information about a skill",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() == 0 {
				return fmt.Errorf("skill name is required")
			}

			skillName := cmd.Args().First()

			cfg, err := config.LoadConfigAllowEmpty()
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()

			sources := skills.DefaultSources(cwd)
			sources = append(sources, skills.AdditionalSources(cfg.SkillPaths, skills.ScopeUser)...)
			sources = append(sources, skills.AdditionalSources(cmd.Root().StringSlice("skill-dir"), skills.ScopeUser)...)

			allSkills, _ := skills.Discover(sources)

			var found *skills.Skill
			for _, s := range allSkills {
				if strings.EqualFold(s.Name, skillName) {
					found = s
					break
				}
			}

			if found == nil {
				return fmt.Errorf("skill %q not found", skillName)
			}

			fmt.Printf("Name:              %s\n", found.Name)
			fmt.Printf("Description:       %s\n", found.Description)
			fmt.Printf("Scope:             %s\n", found.Scope)
			fmt.Printf("Path:              %s\n", found.Path)

			userInv := "yes"
			if !found.UserInvocable {
				userInv = "no"
			}
			fmt.Printf("User-invocable:    %s\n", userInv)

			if found.License != "" {
				fmt.Printf("License:           %s\n", found.License)
			}
			if found.Compatibility != "" {
				fmt.Printf("Compatibility:     %s\n", found.Compatibility)
			}
			if len(found.Metadata) > 0 {
				fmt.Printf("Metadata:\n")
				for k, v := range found.Metadata {
					fmt.Printf("  %s: %s\n", k, v)
				}
			}
			if found.AllowedTools != "" {
				fmt.Printf("Allowed tools:     %s\n", found.AllowedTools)
			}

			// Preview first 200 chars of instructions.
			fmt.Printf("Instructions:\n%s\n", truncateString(found.Instructions, 200))

			return nil
		},
	}
}
