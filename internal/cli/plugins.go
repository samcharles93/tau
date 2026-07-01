package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/plugin/registry"
	urfavecli "github.com/urfave/cli/v3"
)

func pluginsCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "plugin",
		Usage: "Manage plugins from the Tau registry",
		Commands: []*urfavecli.Command{
			pluginSearchCmd(),
			pluginInfoCmd(),
			pluginInstallCmd(),
			pluginListCmd(),
			pluginUninstallCmd(),
			pluginUpdateCmd(),
			pluginPublishCmd(),
			pluginVersionPublishCmd(),
		},
	}
}

func pluginSearchCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "search",
		Usage: "Search the plugin registry",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "category", Usage: "Filter by category"},
			&urfavecli.StringFlag{Name: "tag", Usage: "Filter by tag"},
			&urfavecli.StringFlag{Name: "author", Usage: "Filter by author"},
			&urfavecli.StringFlag{Name: "sort", Usage: "Sort: downloads, updated, name"},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			query := strings.Join(cmd.Args().Slice(), " ")
			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			var tags []string
			if t := cmd.String("tag"); t != "" {
				tags = []string{t}
			}

			result, err := client.Search(
				ctx, query,
				cmd.String("category"),
				tags,
				cmd.String("author"),
				cmd.String("sort"),
				1, 50,
			)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			if len(result.Data) == 0 {
				if query != "" {
					fmt.Printf("No plugins found for %q.\n", query)
				} else {
					fmt.Println("No plugins found.")
				}
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "ID\tNAME\tCATEGORY\tAUTHOR\tVERSION\tDOWNLOADS\n")
			for _, ext := range result.Data {
				verified := ""
				if ext.Verified {
					verified = " ✓"
				}
				latest := ext.Latest
				if latest != "" {
					latest = "v" + latest
				}
				fmt.Fprintf(w, "%s\t%s%s\t%s\t%s\t%s\t%d\n",
					ext.ID, ext.Name, verified, ext.Category, ext.Author, latest, ext.Downloads)
			}
			w.Flush()
			fmt.Printf("\n%d results. Install with: tau plugin install <id>\n", result.Total)
			return nil
		},
	}
}

func pluginInfoCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "info",
		Usage: "Show details for a plugin",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() == 0 {
				return fmt.Errorf("plugin ID is required: tau plugin info <id>")
			}
			id := cmd.Args().First()

			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			ext, err := client.GetExtension(ctx, id)
			if err != nil {
				if registry.IsNotFound(err) {
					return fmt.Errorf("plugin %q not found in registry", id)
				}
				return fmt.Errorf("fetch plugin: %w", err)
			}

			fmt.Printf("  %s\n", ext.Name)
			fmt.Printf("  %s\n\n", strings.Repeat("─", len(ext.Name)+2))
			fmt.Printf("  ID:          %s\n", ext.ID)
			fmt.Printf("  Author:      %s\n", ext.Author)
			if ext.Category != "" {
				fmt.Printf("  Category:    %s\n", ext.Category)
			}
			fmt.Printf("  Repo:        github.com/%s\n", ext.Repo)
			if ext.License != "" {
				fmt.Printf("  License:     %s\n", ext.License)
			}
			if ext.Homepage != "" {
				fmt.Printf("  Homepage:    %s\n", ext.Homepage)
			}
			if ext.Verified {
				fmt.Printf("  Status:      ✓ Verified\n")
			}
			fmt.Printf("  Downloads:   %d\n", ext.Downloads)
			if len(ext.Tags) > 0 {
				fmt.Printf("  Tags:        %s\n", strings.Join(ext.Tags, ", "))
			}
			if ext.MinTauVersion != "" {
				fmt.Printf("  Min tau:     %s\n", ext.MinTauVersion)
			}
			fmt.Printf("\n  %s\n\n", ext.Description)

			// Show installed status.
			if plugin.IsInstalled(id) {
				fmt.Println("  Status:      ✓ installed")
			} else {
				fmt.Println("  Status:      not installed")
			}

			// Show versions and platforms.
			versions, err := client.ListVersions(ctx, id)
			if err != nil {
				return nil // non-fatal
			}
			if len(versions) > 0 {
				fmt.Println("\n  Versions:")
				for _, v := range versions {
					platforms := platformList(ctx, client, id, v.Version)
					fmt.Printf("    v%-12s  %s\n", v.Version, strings.Join(platforms, "  "))
				}
			}

			fmt.Printf("\n  Install:  tau plugin install %s\n", ext.ID)
			return nil
		},
	}
}

func pluginInstallCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "install",
		Usage: "Install a plugin from the registry",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "from", Usage: "Install directly from GitHub: owner/repo:plugin[@version]"},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if from := cmd.String("from"); from != "" {
				spec, err := ParsePluginSourceSpec(from)
				if err != nil {
					return fmt.Errorf("invalid --from: %w", err)
				}
				fmt.Printf("Installing %s from github.com/%s/%s", spec.Plugin, spec.Owner, spec.Repo)
				if spec.Version != "" {
					fmt.Printf("@%s", spec.Version)
				}
				fmt.Printf(" (%s/%s)...\n", runtime.GOOS, runtime.GOARCH)

				_, err = plugin.InstallFromGitHub(ctx, spec.Owner, spec.Repo, spec.Plugin, spec.Version)
				return err
			}

			if cmd.Args().Len() == 0 {
				return fmt.Errorf("plugin ID is required: tau plugin install <id>[@version]")
			}

			raw := cmd.Args().First()
			id := raw
			version := ""

			if before, after, ok := strings.Cut(raw, "@"); ok {
				id = before
				version = after
			}

			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			fmt.Printf("Installing %s", id)
			if version != "" {
				fmt.Printf("@%s", version)
			}
			fmt.Printf(" (%s/%s)...\n", runtime.GOOS, runtime.GOARCH)

			_, err = plugin.Install(ctx, client, id, version)
			return err
		},
	}
}

func pluginListCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "list",
		Usage: "List installed plugins",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			installed, err := plugin.ListInstalled()
			if err != nil {
				return err
			}

			if len(installed) == 0 {
				fmt.Println("No plugins installed.")
				return nil
			}

			fmt.Println("Installed plugins:")
			for _, p := range installed {
				fmt.Printf("  %-30s  %s\n", p.Name, formatSize(p.Size))
			}
			fmt.Printf("\n%d plugin(s) in %s/\n", len(installed), filepath.Join(config.Dir(), "plugins"))
			return nil
		},
	}
}

func pluginUninstallCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "uninstall",
		Usage: "Remove an installed plugin",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() == 0 {
				return fmt.Errorf("plugin ID is required: tau plugin uninstall <id>")
			}
			id := cmd.Args().First()

			if err := plugin.Uninstall(id); err != nil {
				return err
			}

			fmt.Printf("✓ removed %s\n", id)
			fmt.Println("✓ plugins reloaded")
			return nil
		},
	}
}

func pluginUpdateCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "update",
		Usage: "Update installed plugins to the latest version",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			// If an ID is given, update just that plugin.
			if cmd.Args().Len() > 0 {
				id := cmd.Args().First()
				if !plugin.IsInstalled(id) {
					return fmt.Errorf("plugin %q is not installed", id)
				}

				versions, err := client.ListVersions(ctx, id)
				if err != nil {
					return fmt.Errorf("fetch versions: %w", err)
				}
				if len(versions) == 0 {
					return fmt.Errorf("no versions available for %q", id)
				}

				latest := versions[0].Version
				fmt.Printf("%s: updating to v%s...\n", id, latest)
				_, err = plugin.Install(ctx, client, id, latest)
				return err
			}

			// Check all installed plugins.
			installed, err := plugin.ListInstalled()
			if err != nil {
				return err
			}

			var updates []string
			for _, p := range installed {
				versions, err := client.ListVersions(ctx, p.Name)
				if err != nil {
					fmt.Printf("  %-30s  ⚠ could not check: %v\n", p.Name, err)
					continue
				}
				if len(versions) > 0 {
					latest := versions[0].Version
					fmt.Printf("  %-30s  registry has v%s\n", p.Name, latest)
					updates = append(updates, p.Name)
				}
			}

			if len(updates) == 0 {
				fmt.Println("All plugins up to date.")
			} else {
				fmt.Printf("\nRun 'tau plugin update <id>' to upgrade.\n")
			}
			return nil
		},
	}
}

func pluginPublishCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "publish",
		Usage: "Publish a plugin to the registry (requires TAU_REGISTRY_TOKEN)",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "id", Usage: "Explicit extension ID (derived from repo if omitted)"},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			data, err := os.ReadFile("tau.json")
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no tau.json found in current directory")
				}
				return fmt.Errorf("read tau.json: %w", err)
			}

			var manifest struct {
				Name          string   `json:"name"`
				Description   string   `json:"description"`
				Author        string   `json:"author"`
				Repo          string   `json:"repository"`
				Category      string   `json:"category"`
				License       string   `json:"license"`
				Homepage      string   `json:"homepage"`
				Tags          []string `json:"tags"`
				MinTauVersion string   `json:"min_tau_version"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return fmt.Errorf("invalid tau.json: %w", err)
			}

			if manifest.Name == "" || manifest.Description == "" || manifest.Author == "" {
				return fmt.Errorf("tau.json is missing required fields: name, description, author")
			}
			if manifest.Repo == "" && cmd.String("id") == "" {
				return fmt.Errorf("tau.json is missing repository field; either add it or pass --id")
			}

			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			req := &registry.PublishRequest{
				ID:            cmd.String("id"),
				Name:          manifest.Name,
				Description:   manifest.Description,
				Author:        manifest.Author,
				Repo:          manifest.Repo,
				Category:      manifest.Category,
				License:       manifest.License,
				Homepage:      manifest.Homepage,
				Tags:          manifest.Tags,
				MinTauVersion: manifest.MinTauVersion,
			}

			fmt.Printf("Publishing %s...\n", manifest.Name)
			if err := client.PublishExtension(ctx, req); err != nil {
				return fmt.Errorf("publish failed: %w", err)
			}

			fmt.Printf("  ✓ %s published to registry\n", manifest.Name)
			return nil
		},
	}
}

func pluginVersionPublishCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "version-publish",
		Usage: "Publish a plugin version to the registry (requires TAU_REGISTRY_TOKEN)",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "version", Usage: "Semver version (e.g. 0.1.0)", Required: true},
			&urfavecli.StringFlag{Name: "url", Usage: "Release URL for this version"},
			&urfavecli.StringFlag{Name: "notes", Usage: "Release notes (markdown)"},
			&urfavecli.StringFlag{Name: "checksum", Usage: "SHA256 checksum of the source"},
			&urfavecli.Int64Flag{Name: "size", Usage: "Total size in bytes"},
			&urfavecli.StringFlag{Name: "go-version", Usage: "Go version used to build"},
			&urfavecli.StringFlag{Name: "min-tau", Usage: "Minimum tau version required"},
			&urfavecli.StringFlag{Name: "platforms", Usage: "Platforms JSON file (array of {os,arch,url,checksum,size})"},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() == 0 {
				return fmt.Errorf("extension ID is required: tau plugin version-publish <id> --version <ver>")
			}
			id := cmd.Args().First()

			client, err := newRegistryClient()
			if err != nil {
				return err
			}

			var platforms []registry.Platform
			if pf := cmd.String("platforms"); pf != "" {
				data, err := os.ReadFile(pf)
				if err != nil {
					return fmt.Errorf("read platforms file: %w", err)
				}
				if err := json.Unmarshal(data, &platforms); err != nil {
					return fmt.Errorf("invalid platforms file: %w", err)
				}
			}

			req := &registry.CreateVersionRequest{
				Version:       cmd.String("version"),
				Checksum:      cmd.String("checksum"),
				Size:          int64(cmd.Int("size")),
				GoVersion:     cmd.String("go-version"),
				ReleaseURL:    cmd.String("url"),
				ReleaseNotes:  cmd.String("notes"),
				MinTauVersion: cmd.String("min-tau"),
				Platforms:     platforms,
			}

			fmt.Printf("Publishing %s@%s...\n", id, req.Version)
			if err := client.PublishVersion(ctx, id, req); err != nil {
				return fmt.Errorf("version publish failed: %w", err)
			}

			fmt.Printf("  ✓ %s@%s published to registry\n", id, req.Version)
			return nil
		},
	}
}

// ---------- helpers ----------

func newRegistryClient() (*registry.Client, error) {
	cfg, err := config.LoadConfigAllowEmpty()
	if err != nil {
		cfg = config.Config{}
	}

	baseURL := os.Getenv("TAU_REGISTRY")
	if baseURL == "" {
		baseURL = cfg.Registry.URL
	}
	if baseURL == "" {
		baseURL = registry.DefaultRegistry
	}

	token := os.Getenv("TAU_REGISTRY_TOKEN")
	if token == "" {
		token = cfg.Registry.Token
	}

	return registry.NewClient(baseURL, token), nil
}

func platformList(ctx context.Context, client *registry.Client, id, version string) []string {
	ver, err := client.GetVersion(ctx, id, version)
	if err != nil || len(ver.Platforms) == 0 {
		return nil
	}

	var list []string
	for _, p := range ver.Platforms {
		list = append(list, p.OS+"/"+p.Arch)
	}
	return list
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
