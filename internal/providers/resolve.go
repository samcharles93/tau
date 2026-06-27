package providers

import (
	"os"
	"sort"
	"strings"

	"github.com/samcharles93/tau/internal/config"
)

// Source records where a resolved provider came from, for display and so the
// UI can explain why a provider is (or isn't) available.
type Source string

const (
	// SourceConfig is a provider declared in the user's hand-written config.
	SourceConfig Source = "config"
	// SourceEnv is a catalog provider enabled by the user and backed by an
	// environment variable.
	SourceEnv Source = "env"
	// SourceOAuth is a catalog provider authenticated through a login flow.
	SourceOAuth Source = "oauth"
)

// ResolvedProvider is an effective provider tau can build a runtime for, plus
// the metadata needed to display and manage it.
type ResolvedProvider struct {
	Config    config.ProviderConfig
	Source    Source
	EnvVar    string // for env providers: the variable supplying the key
	Available bool   // a usable credential is present right now
}

// MenuEntry is one row in the /login provider menu: every catalog provider plus
// any OAuth providers with stored credentials, annotated with current status so
// the selector can render toggles and login state.
type MenuEntry struct {
	ID          string
	DisplayName string
	Auth        AuthKind
	Source      Source
	Enabled     bool   // user has turned this on (env) or logged in (oauth)
	Available   bool   // credential present right now
	EnvVar      string // for api-key providers: the variable probed
	FromConfig  bool   // also declared in hand-written config (cannot toggle here)
}

func osGetenv(name string) string { return os.Getenv(name) }

// Resolve merges the user's config, the managed state, and the environment into
// the effective set of providers tau should use, in a stable order: config
// providers first (in declared order), then enabled env providers, then OAuth
// providers, both alphabetical. getenv may be nil to use the process
// environment.
func Resolve(cfg config.Config, state State, getenv func(string) string) []ResolvedProvider {
	if getenv == nil {
		getenv = osGetenv
	}

	var out []ResolvedProvider
	declared := make(map[string]struct{}, len(cfg.Providers))

	// 1. Hand-written config providers are authoritative and always active.
	for _, p := range cfg.Providers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		declared[name] = struct{}{}
		out = append(out, ResolvedProvider{
			Config:    p,
			Source:    SourceConfig,
			Available: true,
		})
	}

	// 2. Catalog API-key providers: auto-active when their env var is present
	//    (unless explicitly disabled), plus any the user enabled explicitly.
	//    Config-declared providers win and are skipped here.
	for _, entry := range catalog {
		if entry.Auth == AuthOAuth {
			continue
		}
		if _, ok := declared[entry.ID]; ok {
			continue
		}
		envVar, present := entry.DetectEnvVar(getenv)
		available := present || entry.Auth == AuthNone
		active := (available && !state.IsDisabled(entry.ID)) || state.IsEnabled(entry.ID)
		if !active {
			continue
		}
		out = append(out, ResolvedProvider{
			Config:    apiKeyProviderConfig(entry, envVar),
			Source:    SourceEnv,
			EnvVar:    envVar,
			Available: available,
		})
	}

	// 3. OAuth providers with stored credentials.
	oauthIDs := make([]string, 0, len(state.OAuth))
	for id := range state.OAuth {
		oauthIDs = append(oauthIDs, id)
	}
	sort.Strings(oauthIDs)
	for _, id := range oauthIDs {
		if _, ok := declared[id]; ok {
			continue
		}
		entry, ok := Lookup(id)
		if !ok {
			continue
		}
		creds := state.OAuth[id]
		out = append(out, ResolvedProvider{
			Config:    oauthProviderConfig(entry, creds),
			Source:    SourceOAuth,
			Available: strings.TrimSpace(creds.Access) != "",
		})
	}

	return out
}

// Menu builds the /login provider list: every catalog entry annotated with its
// current enable/availability state, plus any OAuth providers that have creds
// but somehow aren't in the catalog (defensive). getenv may be nil.
func Menu(cfg config.Config, state State, getenv func(string) string) []MenuEntry {
	if getenv == nil {
		getenv = osGetenv
	}
	declared := make(map[string]struct{}, len(cfg.Providers))
	for _, p := range cfg.Providers {
		declared[strings.TrimSpace(p.Name)] = struct{}{}
	}

	out := make([]MenuEntry, 0, len(catalog))
	for _, e := range catalog {
		entry := MenuEntry{
			ID:          e.ID,
			DisplayName: e.DisplayName,
			Auth:        e.Auth,
		}
		_, entry.FromConfig = declared[e.ID]
		switch e.Auth {
		case AuthOAuth:
			creds, ok := state.OAuthFor(e.ID)
			entry.Source = SourceOAuth
			entry.Enabled = ok && strings.TrimSpace(creds.Access) != ""
			entry.Available = entry.Enabled && !creds.Expired()
		default:
			envVar, present := e.DetectEnvVar(getenv)
			available := present || e.Auth == AuthNone
			entry.Source = SourceEnv
			entry.EnvVar = envVar
			entry.Available = available
			// Active = auto-on (key present, not disabled) or explicitly enabled.
			entry.Enabled = (available && !state.IsDisabled(e.ID)) || state.IsEnabled(e.ID)
		}
		out = append(out, entry)
	}
	return out
}

// apiKeyProviderConfig builds a config.ProviderConfig for a catalog API-key
// provider, referencing the detected environment variable.
func apiKeyProviderConfig(entry CatalogEntry, envVar string) config.ProviderConfig {
	auth := config.AuthConfig{Type: config.AuthTypeAPIKey, APIKeyEnv: envVar}
	if entry.Auth == AuthNone {
		auth = config.AuthConfig{Type: config.AuthTypeNone}
	}
	return config.ProviderConfig{
		Name:    entry.ID,
		Type:    entry.Class,
		BaseURL: entry.BaseURL,
		Auth:    auth,
	}
}

// oauthProviderConfig builds a config.ProviderConfig for a logged-in OAuth
// provider. The stored access token is supplied as a literal api_key so the
// existing runtime treats it as a bearer token; token refresh and any derived
// base URL are layered on in the provider-specific login phases.
func oauthProviderConfig(entry CatalogEntry, creds OAuthCredentials) config.ProviderConfig {
	baseURL := entry.BaseURL
	if v := strings.TrimSpace(creds.Extra["base_url"]); v != "" {
		baseURL = v
	}
	return config.ProviderConfig{
		Name:    entry.ID,
		BaseURL: baseURL,
		Auth: config.AuthConfig{
			Type:   config.AuthTypeAPIKey,
			APIKey: creds.Access,
		},
	}
}
