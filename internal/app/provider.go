package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// ProviderStatus is one provider-list row prepared for presentation by a CLI
// or other frontend.
type ProviderStatus struct {
	ID      string
	Status  string
	Source  string
	Auth    string
	Details string
}

// ProviderLoginOption is one provider that supports an interactive login.
type ProviderLoginOption struct {
	ID    string
	Label string
}

// ProviderLoginOptions configures a single provider authentication flow.
type ProviderLoginOptions struct {
	ProviderID       string
	EnterpriseDomain string
	Prompter         SetupPrompter
	Stdout           io.Writer
	// Insecure skips TLS certificate verification on the live API-key
	// validation call, mirroring RunSetup's --insecure flag for users
	// behind a TLS-intercepting proxy.
	Insecure bool

	loginOAuth oauthLoginFunc
}

// ProviderLoginResult identifies the provider authenticated by RunProviderLogin.
type ProviderLoginResult struct {
	ProviderID   string
	ProviderName string
}

type oauthLoginFunc func(context.Context, string, providers.LoginOptions) (providers.OAuthCredentials, error)

// ListProviders returns the current provider catalogue with resolved status
// and credential source labels.
func ListProviders() ([]ProviderStatus, error) {
	return listProviders(nil)
}

func listProviders(getenv func(string) string) ([]ProviderStatus, error) {
	cfg, err := config.LoadConfigAllowEmpty()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	state, err := providers.LoadState()
	if err != nil {
		return nil, fmt.Errorf("load provider state: %w", err)
	}
	menu := providers.Menu(cfg, state, getenv)
	statuses := make([]ProviderStatus, 0, len(menu))
	for _, entry := range menu {
		status := "disabled"
		switch {
		case entry.FromConfig:
			status = "configured"
		case entry.Enabled && entry.Available:
			status = "enabled"
		case entry.Enabled:
			status = "unavailable"
		}

		source := string(entry.Source)
		if entry.FromConfig {
			source = string(providers.SourceConfig)
		} else if entry.Source == providers.SourceEnv && entry.Enabled && entry.EnvVar != "" {
			source += ":$" + entry.EnvVar
		}
		statuses = append(statuses, ProviderStatus{
			ID:      entry.ID,
			Status:  status,
			Source:  source,
			Auth:    string(entry.Auth),
			Details: entry.Message,
		})
	}
	return statuses, nil
}

// ProviderLoginChoices returns every OAuth or API-key provider that can
// be authenticated through RunProviderLogin.
func ProviderLoginChoices() []ProviderLoginOption {
	entries := providers.Catalog()
	options := make([]ProviderLoginOption, 0, len(entries))
	for _, entry := range entries {
		if entry.Auth == providers.AuthNone {
			continue
		}
		options = append(options, ProviderLoginOption{
			ID:    entry.ID,
			Label: fmt.Sprintf("%s (%s)", entry.DisplayName, entry.Auth),
		})
	}
	return options
}

// RunProviderLogin authenticates one provider and persists the resulting
// managed credential through providers.Manage.
func RunProviderLogin(ctx context.Context, opts ProviderLoginOptions) (ProviderLoginResult, error) {
	entry, ok := providers.Lookup(opts.ProviderID)
	if !ok {
		return ProviderLoginResult{}, fmt.Errorf("unknown provider %q", strings.TrimSpace(opts.ProviderID))
	}
	if opts.Prompter == nil {
		return ProviderLoginResult{}, errors.New("provider login: no prompter configured")
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	loginOAuth := opts.loginOAuth
	if loginOAuth == nil {
		loginOAuth = providers.LoginOAuth
	}
	manage := providers.NewManage(nil)

	switch entry.Auth {
	case providers.AuthOAuth:
		if err := authenticateProviderOAuth(ctx, stdout, manage, entry, opts.EnterpriseDomain, loginOAuth); err != nil {
			return ProviderLoginResult{}, err
		}
	case providers.AuthAPIKey:
		if err := authenticateProviderAPIKey(ctx, RunSetupOptions{Prompter: opts.Prompter, Stdout: stdout, Insecure: opts.Insecure}, manage, entry); err != nil {
			return ProviderLoginResult{}, err
		}
	default:
		return ProviderLoginResult{}, fmt.Errorf("%s does not require login", entry.DisplayName)
	}
	return ProviderLoginResult{ProviderID: entry.ID, ProviderName: entry.DisplayName}, nil
}

// LogoutProvider disables a provider and removes its managed credentials.
func LogoutProvider(providerID string) (string, error) {
	entry, ok := providers.Lookup(providerID)
	if !ok {
		return "", fmt.Errorf("unknown provider %q", strings.TrimSpace(providerID))
	}
	if err := providers.NewManage(nil).Logout(entry.ID); err != nil {
		return "", err
	}
	return entry.DisplayName, nil
}
