package tui

import (
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/pkg/taui"
)

// argProviders offers the catalog provider IDs as completions for the first
// argument of /login and /logout.
func argProviders(c *inlineChat, _ []string, argsBefore int) (string, []taui.Match) {
	if argsBefore != 0 {
		return "", nil
	}
	menu := providers.Menu(c.providerCfg(), c.providerState(), nil)
	out := make([]taui.Match, 0, len(menu))
	for _, e := range menu {
		desc := e.DisplayName
		if e.Enabled {
			desc += " (enabled)"
		}
		out = append(out, taui.Match{Word: e.ID, Description: desc})
	}
	return "Providers", out
}

// handleLoginCommand enables a provider. With no argument it prints the menu of
// known providers and their current state. OAuth (subscription) providers are
// not yet wired up and report as such.
func (c *inlineChat) handleLoginCommand(args string) {
	name := strings.ToLower(strings.TrimSpace(args))
	if name == "" {
		c.printProviderMenu()
		return
	}

	entry, ok := providers.Lookup(name)
	if !ok {
		c.engine.PrintAbove("%s %s", c.grey("✗"), fmt.Sprintf("unknown provider %q — see /login for the list", name))
		return
	}
	if entry.Auth == providers.AuthOAuth {
		c.engine.PrintAbove("%s %s", c.grey("ℹ"), fmt.Sprintf("%s uses an interactive login that isn't wired up yet", entry.DisplayName))
		return
	}

	state, err := providers.LoadState()
	if err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "load provider state: "+err.Error())
		return
	}
	state.Enable(entry.ID)
	if err := state.Save(); err != nil {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "save provider state: "+err.Error())
		return
	}

	if envVar, present := entry.DetectEnvVar(nil); present {
		c.pushNotice("enabled " + entry.ID)
		_ = envVar
	} else if envVar != "" {
		c.engine.PrintAbove("%s %s", c.grey("ℹ"), fmt.Sprintf("enabled %s, but no API key found — set $%s", entry.ID, envVar))
	} else {
		c.pushNotice("enabled " + entry.ID)
	}
	c.providersChanged()
}

// handleLogoutCommand disables an API-key provider or removes stored OAuth
// credentials for the named provider.
func (c *inlineChat) handleLogoutCommand(args string) {
	name := strings.ToLower(strings.TrimSpace(args))
	if name == "" {
		c.engine.PrintAbove("%s %s", c.grey("✗"), "usage: /logout <provider>")
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
	c.pushNotice("disabled " + entry.ID)
	c.providersChanged()
}

// printProviderMenu renders the catalog with each provider's current state,
// mirroring the selector pi shows for /login.
func (c *inlineChat) printProviderMenu() {
	menu := providers.Menu(c.providerCfg(), c.providerState(), nil)
	var b strings.Builder
	b.WriteString("Providers (use /login <name> to enable, /logout <name> to disable):")
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

// providersChanged refreshes the aggregated model list so models from a
// provider just enabled (or removed when disabled) take effect immediately.
// Switching to one with /model then routes the next turn there live, no restart
// required.
func (c *inlineChat) providersChanged() {
	c.refreshModels()
	c.engine.PrintAbove("%s", c.grey("saved — /model to pick from the updated model list"))
}

func (c *inlineChat) providerCfg() config.Config { cfg, _, _ := providers.Effective(); return cfg }

func (c *inlineChat) providerState() providers.State {
	s, _ := providers.LoadState()
	return s
}
