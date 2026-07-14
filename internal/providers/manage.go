package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/config"
)

// Manage provides the provider lifecycle operations shared by the CLI, both
// TUIs, and the setup wizard: toggling an env-var/keyless provider on or off,
// completing an OAuth login, logging out, and re-resolving the effective
// provider set. Each method loads state fresh and saves it before returning,
// so a Manage instance carries no session state of its own — the caller
// doesn't need to sequence calls or worry about staleness.
//
// The OAuth device-code flow itself (LoginOAuth, BeginOAuthLogin,
// RefreshOAuth) is not wrapped here: the legacy TUI drives it with a blocking
// goroutine and tui2 drives it with a two-phase Bubbletea Cmd, and those
// async patterns are TUI-specific. LoginComplete only covers the part both
// patterns converge on afterwards: persisting the resulting credentials.
type Manage struct {
	getenv func(string) string
}

// NewManage builds a Manage service. getenv may be nil to use the process
// environment.
func NewManage(getenv func(string) string) *Manage {
	if getenv == nil {
		getenv = osGetenv
	}
	return &Manage{getenv: getenv}
}

// Toggle enables an API-key/no-auth provider if it's currently off, or
// disables it if it's currently on — the effective on/off state as resolved
// by env-detection and Enabled/Disabled, not just the raw explicit-enable
// list, so toggling an auto-detected (env-key-present) provider off actually
// takes effect. OAuth providers and providers with a stored managed key
// aren't toggleable this way: OAuth needs LoginComplete/Logout, and a
// managed key's presence is itself the enabled signal (see Logout).
func (m *Manage) Toggle(name string) (enabled bool, warning string, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	entry, ok := Lookup(name)
	if !ok {
		return false, "", fmt.Errorf("unknown provider %q", name)
	}
	if entry.Auth == AuthOAuth {
		return false, "", fmt.Errorf("%s uses OAuth login, not toggle — use LoginComplete/Logout", entry.DisplayName)
	}

	state, err := LoadState()
	if err != nil {
		return false, "", fmt.Errorf("load provider state: %w", err)
	}
	if key, ok := state.APIKeyFor(entry.ID); ok && strings.TrimSpace(key) != "" {
		return false, "", fmt.Errorf("%s has a stored API key — use Logout to remove it, not Toggle", entry.DisplayName)
	}

	envVar, present := entry.DetectEnvVar(m.getenv)
	available := present || entry.Auth == AuthNone
	active := (available && !state.IsDisabled(entry.ID)) || state.IsEnabled(entry.ID)

	if active {
		state.Disable(entry.ID)
		enabled = false
	} else {
		state.Enable(entry.ID)
		enabled = true
		if !available && envVar != "" {
			warning = envVar + " is not set"
		}
	}
	if err := state.Save(); err != nil {
		return false, "", fmt.Errorf("save provider state: %w", err)
	}
	return enabled, warning, nil
}

// LoginComplete persists OAuth credentials obtained by a login flow and
// enables the provider. It does not perform the login flow itself.
func (m *Manage) LoginComplete(name string, creds OAuthCredentials) error {
	name = strings.ToLower(strings.TrimSpace(name))
	entry, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown provider %q", name)
	}
	if entry.Auth != AuthOAuth {
		return fmt.Errorf("%s doesn't use OAuth login", entry.DisplayName)
	}

	state, err := LoadState()
	if err != nil {
		return fmt.Errorf("load provider state: %w", err)
	}
	state.Enable(entry.ID)
	state.SetOAuth(entry.ID, creds)
	if err := state.Save(); err != nil {
		return fmt.Errorf("save provider state: %w", err)
	}
	return nil
}

// Logout disables a provider and clears all its stored credentials, OAuth
// and managed API key alike, regardless of which kind the provider uses.
// Disabling matters for env-var-backed providers, where there's no
// credential to remove — suppressing auto-detection is the only way to turn
// them off. Removing both credential kinds matters for OAuth and managed-key
// providers, where a stored credential (not Disabled) is what makes them
// active in the first place.
func (m *Manage) Logout(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	entry, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown provider %q", name)
	}

	state, err := LoadState()
	if err != nil {
		return fmt.Errorf("load provider state: %w", err)
	}
	state.Disable(entry.ID)
	state.RemoveOAuth(entry.ID)
	state.RemoveAPIKey(entry.ID)
	if err := state.Save(); err != nil {
		return fmt.Errorf("save provider state: %w", err)
	}
	return nil
}

// Effective re-resolves the effective provider set: the user's config, the
// managed state, and the live environment merged into what tau can actually
// use right now.
func (m *Manage) Effective(ctx context.Context) (config.Config, []ResolvedProvider, error) {
	return effective(ctx, m.getenv)
}
