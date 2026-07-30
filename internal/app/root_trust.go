package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/spec"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/trust"
)

// rootSpecDisplay carries the resolved-root-spec facts the caller shows in
// the startup log and TUI status area, per docs/specs/agents/
// 01-agent-spec-format.md (Root-spec override trust: "Display before
// execution").
type rootSpecDisplay struct {
	// Scope is "builtin", "user", or "project".
	Scope string
	// Source is the file path a filesystem-loaded spec was parsed from;
	// empty for built-ins.
	Source string
	// Hash is the hex sha256 of Source's contents; empty for built-ins.
	Hash string
	// Trusted is true when Source's override is in effect (built-ins and
	// user-scope specs are always "trusted" - no untrusted-repo risk).
	Trusted bool
}

// resolveRootSpecWithTrust resolves the root agent's spec and, for a
// project-level override, applies the trust-on-first-use gate before
// returning it. A rejected or unapproved project override falls back to
// the built-in "tau" spec rather than failing startup - matching the
// spec's documented "N: reject. Fall back to the built-in tau spec."
// Returns an error only for the headless-mode "no way to prompt" case, or
// an unexpected I/O failure reading/hashing the spec file or trust store.
func resolveRootSpecWithTrust(cwd string, opts ChatOptions) (*spec.Definition, rootSpecDisplay, error) {
	def := agent.ResolveRootSpec(cwd)
	if def == nil {
		return nil, rootSpecDisplay{}, fmt.Errorf("resolve root spec: built-in %q not found", "tau")
	}

	display := rootSpecDisplay{Scope: spec.ScopeString(def.Scope), Source: def.SourcePath}

	if def.Scope != skills.ScopeProject || strings.TrimSpace(def.SourcePath) == "" {
		// Built-in or user-level: the user's own machine, not a cloned
		// repository - no privilege-escalation risk to gate.
		display.Trusted = true
		return def, display, nil
	}

	hash, err := trust.HashFile(def.SourcePath)
	if err != nil {
		return nil, rootSpecDisplay{}, fmt.Errorf("root-spec trust: %w", err)
	}
	display.Hash = hash

	store, err := trust.Load(tauconfig.Dir())
	if err != nil {
		return nil, rootSpecDisplay{}, fmt.Errorf("root-spec trust: %w", err)
	}

	if store.IsTrusted(cwd, hash) {
		display.Trusted = true
		return def, display, nil
	}

	if opts.TrustProjectRootSpec {
		// --trust-project-root-spec / TAU_TRUST_PROJECT_ROOT_SPEC (bound
		// via the CLI flag's EnvVars source): trusted for this invocation
		// only, not persisted to trust.yaml.
		display.Trusted = true
		return def, display, nil
	}

	if !isInteractiveTTY() {
		return nil, rootSpecDisplay{}, fmt.Errorf(
			"project root-spec override at %s requires trust approval; run interactively first or add to %s",
			def.SourcePath, filepath.Join(tauconfig.Dir(), "trust.yaml"),
		)
	}

	approved, mode := promptRootSpecTrust(def, hash)
	if !approved {
		builtin := agent.ResolveBuiltinTau()
		if builtin == nil {
			return nil, rootSpecDisplay{}, fmt.Errorf("root-spec trust: rejected override and built-in %q unavailable", "tau")
		}
		return builtin, rootSpecDisplay{Scope: spec.ScopeString(builtin.Scope), Trusted: true}, nil
	}

	store.Trust(cwd, hash, mode)
	if err := store.Save(); err != nil {
		return nil, rootSpecDisplay{}, fmt.Errorf("root-spec trust: save: %w", err)
	}
	display.Trusted = true
	return def, display, nil
}

// isInteractiveTTY reports whether stdin is a real terminal - the
// prerequisite for showing an interactive y/N/a prompt. CI runs, piped
// stdin, and other headless invocations report false. A package var (not
// a plain func) so tests can force either branch deterministically instead
// of depending on the test runner's own stdin, which may or may not be a
// TTY depending on how `go test` was invoked.
var isInteractiveTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptRootSpecTrust shows the resolved override's scope/source/hash and
// asks the user to approve it, per docs/specs/agents/
// 01-agent-spec-format.md (Trust-on-first-use with content binding). Prints
// to stderr so it never contaminates stdout. Any read failure or
// unrecognised input is treated as "N" (reject) - a security prompt must
// fail closed. A package var so tests can stub the interactive prompt.
var promptRootSpecTrust = func(def *spec.Definition, hash string) (approved bool, mode string) {
	fmt.Fprintf(os.Stderr, "Project %s overrides the root agent. Trust this override? [y/N/a] ", def.SourcePath)
	fmt.Fprintf(os.Stderr, "\n  scope: project\n  source: %s\n  hash: sha256:%s\n> ", def.SourcePath, hash)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, ""
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "y":
		return true, trust.ModeHash
	case "a":
		return true, trust.ModePath
	default:
		return false, ""
	}
}
