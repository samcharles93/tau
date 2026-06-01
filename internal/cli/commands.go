package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samcharles93/tau/internal/app"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
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
	sessionsDir := filepath.Join(config.Dir(), "sessions")
	storePath := filepath.Join(config.Dir(), "sessions.db")
	s, err := store.NewSQLiteStore(storePath, sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("opening session store: %w", err)
	}
	return s, nil
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
