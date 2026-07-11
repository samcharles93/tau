package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// argProvider offers completions for /provider's argument(s): argsBefore==0
// offers the catalog provider IDs directly (a bare "/provider <name>"
// toggles it) plus the "login"/"logout" sub-verbs; argsBefore==1, when the
// first word is "login" or "logout", offers the catalog provider IDs again
// as that sub-verb's argument.
func argProvider(c *inlineChat, fields []string, argsBefore int) (string, []taui.Match) {
	menu := providers.Menu(c.providerCfg(), c.providerState(), nil)
	providerMatches := make([]taui.Match, 0, len(menu))
	for _, e := range menu {
		desc := e.DisplayName
		if e.Enabled {
			desc += " (enabled)"
		}
		providerMatches = append(providerMatches, taui.Match{Word: e.ID, Description: desc})
	}

	if argsBefore == 0 {
		matches := append([]taui.Match{
			{Word: "login", Description: "OAuth sign-in for a provider"},
			{Word: "logout", Description: "disable a provider / remove its login"},
		}, providerMatches...)
		return "Providers", matches
	}
	if argsBefore == 1 && len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "login", "logout":
			return "Providers", providerMatches
		}
	}
	return "", nil
}

// handleProviderCommand dispatches /provider's three forms: bare (print the
// menu), a provider name (toggle it on/off), or a "login"/"logout" sub-verb
// followed by a name. There is no catalog provider literally named "login"
// or "logout", so the sub-verb check is unambiguous.
func (c *inlineChat) handleProviderCommand(args string) {
	args = strings.TrimSpace(args)
	if args == "" {
		c.printProviderMenu()
		return
	}

	fields := strings.Fields(args)
	rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	switch strings.ToLower(fields[0]) {
	case "login":
		c.handleProviderLogin(rest)
	case "logout":
		c.handleProviderLogout(rest)
	default:
		c.handleProviderToggle(fields[0])
	}
}

// handleProviderToggle enables an API-key/no-auth provider if it's
// currently off, or disables it if it's currently on — the effective on/off
// state as shown in the /provider menu, not just the raw explicit-enable
// list, so toggling an auto-detected (env-key-present) provider off
// actually takes effect. OAuth providers aren't toggleable this way; they
// need /provider login.
func (c *inlineChat) handleProviderToggle(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	entry, ok := providers.Lookup(name)
	if !ok {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("unknown provider %q — see /provider for the list", name))
		return
	}
	if entry.Auth == providers.AuthOAuth {
		c.engine.PrintAbove("%s %s", c.grey("ℹ"), fmt.Sprintf("%s uses OAuth — use /provider login %s", entry.DisplayName, entry.ID))
		return
	}

	state, err := providers.LoadState()
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "load provider state: "+err.Error())
		return
	}

	enabled := false
	for _, e := range providers.Menu(c.providerCfg(), state, nil) {
		if e.ID == entry.ID {
			enabled = e.Enabled
			break
		}
	}

	var warning string
	if enabled {
		state.Disable(entry.ID)
	} else {
		state.Enable(entry.ID)
		if envVar, present := entry.DetectEnvVar(os.Getenv); !present && envVar != "" {
			warning = "no API key found — set $" + envVar
		}
	}
	if err := state.Save(); err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "save provider state: "+err.Error())
		return
	}
	c.refreshAfterProviderChange(entry.DisplayName, !enabled, warning)
}

// handleProviderLogin handles /provider login <name> [enterprise-domain].
func (c *inlineChat) handleProviderLogin(args string) {
	fields := strings.Fields(args)
	name := ""
	if len(fields) > 0 {
		name = strings.ToLower(strings.TrimSpace(fields[0]))
	}
	if name == "" {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /provider login <provider>")
		return
	}
	entry, ok := providers.Lookup(name)
	if !ok {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("unknown provider %q", name))
		return
	}
	if entry.Auth != providers.AuthOAuth {
		c.engine.PrintAbove("%s %s", c.grey("ℹ"), fmt.Sprintf("%s doesn't use OAuth — use /provider %s to toggle it", entry.DisplayName, entry.ID))
		return
	}
	enterpriseDomain := ""
	if len(fields) > 1 {
		enterpriseDomain = fields[1]
	}
	c.engine.PrintAbove("%s", c.grey(providerui.StartMessage(entry.DisplayName)))
	go func() {
		creds, err := providers.LoginOAuth(c.ctx, entry.ID, providers.LoginOptions{
			EnterpriseDomain: enterpriseDomain,
			OnDeviceCode: func(code providers.DeviceCode) {
				opened := providerui.TryOpenBrowser(c.ctx, code.VerificationURI)
				copied := c.copyProviderLoginCode(code.UserCode)
				c.engine.PrintAbove("%s", c.grey(providerui.DeviceCodeMessage(entry.DisplayName, code, opened, copied)))
			},
		})
		if err != nil {
			c.engine.PrintAbove("%s %s", c.grey("✗"), providerui.FailureMessage(entry.DisplayName, err))
			return
		}
		state, err := providers.LoadState()
		if err != nil {
			c.engine.PrintAbove("%s %s", c.grey("✗"), "load provider state: "+err.Error())
			return
		}
		state.Enable(entry.ID)
		state.SetOAuth(entry.ID, creds)
		if err := state.Save(); err != nil {
			c.engine.PrintAbove("%s %s", c.grey("✗"), "save provider state: "+err.Error())
			return
		}
		c.refreshAfterProviderChange(entry.DisplayName, true, "")
	}()
}

func (c *inlineChat) copyProviderLoginCode(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	seq, ok := termkit.OSC52Copy(code)
	if !ok {
		return false
	}
	c.engine.Terminal.Write(seq)
	return true
}

// handleProviderLogout handles /provider logout <name> — disables the
// provider and clears any stored OAuth credentials, for any provider kind.
func (c *inlineChat) handleProviderLogout(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /provider logout <provider>")
		return
	}
	entry, ok := providers.Lookup(name)
	if !ok {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("unknown provider %q", name))
		return
	}
	state, err := providers.LoadState()
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "load provider state: "+err.Error())
		return
	}
	state.Disable(entry.ID)
	state.RemoveOAuth(entry.ID)
	if err := state.Save(); err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "save provider state: "+err.Error())
		return
	}
	c.refreshAfterProviderChange(entry.DisplayName, false, "")
}

// printProviderMenu renders the catalog with each provider's current state,
// mirroring the selector pi shows for /provider.
func (c *inlineChat) printProviderMenu() {
	menu := providers.Menu(c.providerCfg(), c.providerState(), nil)
	var b strings.Builder
	b.WriteString("Providers (use /provider <name> to toggle, /provider login <name> for OAuth):")
	for _, e := range menu {
		mark := "○"
		if e.Enabled {
			mark = "●"
		}
		note := ""
		switch {
		case e.Auth == providers.AuthOAuth:
			note = "login required"
		case e.FromConfig:
			note = "from config"
		case e.Available:
			note = "key detected"
		case e.EnvVar != "":
			note = "set $" + e.EnvVar
		}
		fmt.Fprintf(&b, "\n  %s %-14s %s", mark, e.ID, note)
	}
	c.engine.PrintAbove("%s", c.grey(b.String()))
}

// refreshAfterProviderChange re-discovers models after a provider toggle and
// prints a single combined scrollback line reporting both the toggle
// outcome and the resulting model count (e.g. "OpenRouter enabled, models
// available: 42") once the (async) refresh completes — a separate toggle
// notice and a separate "refreshed: N models" notice would otherwise race
// each other or simply show as two disconnected messages.
func (c *inlineChat) refreshAfterProviderChange(displayName string, enabled bool, warning string) {
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	if c.refresh == nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("%s %s (model refresh is not available)", displayName, action))
		return
	}
	go func() {
		models, err := c.refresh(c.ctx)
		if err != nil {
			c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("%s %s, but model refresh failed: %s", displayName, action, err.Error()))
			return
		}
		c.mu.Lock()
		c.availableModels = slices.Clone(models)
		c.mu.Unlock()
		line := fmt.Sprintf("%s %s, models available: %d", displayName, action, len(models))
		if warning != "" {
			line += " (" + warning + ")"
		}
		c.engine.PrintAbove("%s", c.grey(line))
		c.engine.RequestRender()
	}()
}

// providerCfg re-resolves provider config. inlineChat (the legacy TUI) has no
// request-scoped context to thread through, so this uses Background directly.
func (c *inlineChat) providerCfg() config.Config {
	cfg, _, _ := providers.Effective(context.Background())
	return cfg
}

func (c *inlineChat) providerState() providers.State {
	s, _ := providers.LoadState()
	return s
}
