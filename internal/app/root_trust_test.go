package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/trust"
)

// withIsolatedConfigDir points HOME/XDG_CONFIG_HOME at a temp directory for
// the duration of the test, so trust.yaml reads/writes — and UserSources()
// discovery — never touch the real user's machine. Returns the resolved
// tau config dir.
func withIsolatedConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return config.Dir()
}

// writeProjectRootSpec writes a project-level tau.agent.md override at the
// path spec.ProjectSources(cwd) actually discovers
// (<cwd>/.agents/agents/tau.agent.md), so resolveRootSpecWithTrust's real
// call to agent.ResolveRootSpec(cwd) picks it up exactly as production
// startup would.
func writeProjectRootSpec(t *testing.T, cwd, bodyMarker string) string {
	t.Helper()
	dir := filepath.Join(cwd, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, "tau.agent.md")
	content := "---\nname: tau\ndescription: test project override\n---\n" + bodyMarker + "\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return p
}

// --- G14: root-spec override trust gate ---

// TestResolveRootSpecWithTrust_BuiltinNeedsNoTrust proves the common case
// (no project override present) never touches the trust store or prompts.
func TestResolveRootSpecWithTrust_BuiltinNeedsNoTrust(t *testing.T) {
	withIsolatedConfigDir(t)
	cwd := t.TempDir() // no .agents/agents/tau.agent.md here

	promptCalled := false
	origPrompt := promptRootSpecTrust
	promptRootSpecTrust = func(*spec.Definition, string) (bool, string) {
		promptCalled = true
		return false, ""
	}
	defer func() { promptRootSpecTrust = origPrompt }()

	resolved, display, err := resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err != nil {
		t.Fatalf("resolveRootSpecWithTrust: %v", err)
	}
	if resolved.Scope != "" {
		t.Errorf("resolved.Scope = %v, want zero value (built-in defs leave Scope unset)", resolved.Scope)
	}
	if !display.Trusted {
		t.Error("built-in scope should always be Trusted=true")
	}
	if promptCalled {
		t.Error("built-in scope must never prompt")
	}
}

// TestResolveRootSpecWithTrust_AlreadyTrustedSkipsPrompt proves a
// previously approved hash is honored without prompting.
func TestResolveRootSpecWithTrust_AlreadyTrustedSkipsPrompt(t *testing.T) {
	configDir := withIsolatedConfigDir(t)
	cwd := t.TempDir()
	specPath := writeProjectRootSpec(t, cwd, "trusted content")
	hash, err := trust.HashFile(specPath)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	store, err := trust.Load(configDir)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	store.Trust(cwd, hash, trust.ModeHash)
	if err := store.Save(); err != nil {
		t.Fatalf("save store: %v", err)
	}

	promptCalled := false
	origPrompt := promptRootSpecTrust
	promptRootSpecTrust = func(*spec.Definition, string) (bool, string) {
		promptCalled = true
		return false, ""
	}
	defer func() { promptRootSpecTrust = origPrompt }()

	resolved, display, err := resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err != nil {
		t.Fatalf("resolveRootSpecWithTrust: %v", err)
	}
	if resolved.Scope != skills.ScopeProject {
		t.Errorf("resolved.Scope = %v, want ScopeProject (trusted override should be used)", resolved.Scope)
	}
	if !display.Trusted {
		t.Error("previously trusted hash should be Trusted=true")
	}
	if promptCalled {
		t.Error("already-trusted hash must not prompt again")
	}
}

// TestResolveRootSpecWithTrust_BypassFlagDoesNotPersist proves
// --trust-project-root-spec trusts for this invocation only — it must not
// write a trust.yaml entry (the flag/env var has to be supplied every
// time, or the user approves interactively to persist it).
func TestResolveRootSpecWithTrust_BypassFlagDoesNotPersist(t *testing.T) {
	configDir := withIsolatedConfigDir(t)
	cwd := t.TempDir()
	writeProjectRootSpec(t, cwd, "bypass content")

	resolved, display, err := resolveRootSpecWithTrust(cwd, ChatOptions{TrustProjectRootSpec: true})
	if err != nil {
		t.Fatalf("resolveRootSpecWithTrust: %v", err)
	}
	if resolved.Scope != skills.ScopeProject || !display.Trusted {
		t.Errorf("expected the override to be used and trusted, got scope=%v trusted=%v", resolved.Scope, display.Trusted)
	}

	store, err := trust.Load(configDir)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if len(store.TrustedSpecs) != 0 {
		t.Error("bypass flag must not persist a trust.yaml entry")
	}
}

// TestResolveRootSpecWithTrust_HeadlessUntrustedFails proves an untrusted
// project override with no interactive TTY and no bypass fails startup
// rather than silently falling back or hanging (per docs/specs/agents/
// 01-agent-spec-format.md, Headless mode).
func TestResolveRootSpecWithTrust_HeadlessUntrustedFails(t *testing.T) {
	withIsolatedConfigDir(t)
	cwd := t.TempDir()
	writeProjectRootSpec(t, cwd, "headless content")

	origTTY := isInteractiveTTY
	isInteractiveTTY = func() bool { return false }
	defer func() { isInteractiveTTY = origTTY }()

	_, _, err := resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err == nil {
		t.Fatal("expected an error for untrusted override with no TTY and no bypass")
	}
}

// TestResolveRootSpecWithTrust_RejectedPromptFallsBackToBuiltin proves an
// explicit "N" doesn't fail startup — it falls back to the built-in tau
// spec, per the spec's documented behavior.
func TestResolveRootSpecWithTrust_RejectedPromptFallsBackToBuiltin(t *testing.T) {
	withIsolatedConfigDir(t)
	cwd := t.TempDir()
	writeProjectRootSpec(t, cwd, "rejected content")

	origTTY := isInteractiveTTY
	isInteractiveTTY = func() bool { return true }
	defer func() { isInteractiveTTY = origTTY }()

	origPrompt := promptRootSpecTrust
	promptRootSpecTrust = func(*spec.Definition, string) (bool, string) { return false, "" }
	defer func() { promptRootSpecTrust = origPrompt }()

	resolved, display, err := resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err != nil {
		t.Fatalf("resolveRootSpecWithTrust: %v", err)
	}
	if !display.Trusted {
		t.Error("fallback to built-in should report Trusted=true (built-in is always trusted)")
	}
	if resolved.Scope != "" {
		t.Errorf("resolved.Scope = %v, want zero value (rejected override should fall back to built-in)", resolved.Scope)
	}
}

// TestResolveRootSpecWithTrust_ApprovedPromptPersists proves an explicit
// "y" both proceeds with the override and writes a trust.yaml entry so
// future runs don't re-prompt.
func TestResolveRootSpecWithTrust_ApprovedPromptPersists(t *testing.T) {
	configDir := withIsolatedConfigDir(t)
	cwd := t.TempDir()
	specPath := writeProjectRootSpec(t, cwd, "approved content")

	origTTY := isInteractiveTTY
	isInteractiveTTY = func() bool { return true }
	defer func() { isInteractiveTTY = origTTY }()

	origPrompt := promptRootSpecTrust
	promptRootSpecTrust = func(*spec.Definition, string) (bool, string) { return true, trust.ModeHash }
	defer func() { promptRootSpecTrust = origPrompt }()

	resolved, display, err := resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err != nil {
		t.Fatalf("resolveRootSpecWithTrust: %v", err)
	}
	if !display.Trusted || resolved.Scope != skills.ScopeProject {
		t.Errorf("expected the project override to be used and trusted, got scope=%v trusted=%v", resolved.Scope, display.Trusted)
	}

	store, err := trust.Load(configDir)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	hash, _ := trust.HashFile(specPath)
	if !store.IsTrusted(cwd, hash) {
		t.Error("approving the prompt should persist a trust.yaml entry")
	}
}

// TestResolveRootSpecWithTrust_EditedSpecRequiresReapproval proves hash-mode
// trust is content-bound: editing the trusted file after approval must
// require re-approval, not silently keep trusting under the old hash.
func TestResolveRootSpecWithTrust_EditedSpecRequiresReapproval(t *testing.T) {
	configDir := withIsolatedConfigDir(t)
	cwd := t.TempDir()
	specPath := writeProjectRootSpec(t, cwd, "original content")
	origHash, _ := trust.HashFile(specPath)

	store, err := trust.Load(configDir)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	store.Trust(cwd, origHash, trust.ModeHash)
	if err := store.Save(); err != nil {
		t.Fatalf("save store: %v", err)
	}

	// Edit the spec file — its hash changes, invalidating the trust entry.
	writeProjectRootSpec(t, cwd, "MODIFIED content — this should require re-approval")

	origTTY := isInteractiveTTY
	isInteractiveTTY = func() bool { return false }
	defer func() { isInteractiveTTY = origTTY }()

	_, _, err = resolveRootSpecWithTrust(cwd, ChatOptions{})
	if err == nil {
		t.Fatal("editing the trusted spec file should invalidate hash-mode trust and require re-approval")
	}
}
