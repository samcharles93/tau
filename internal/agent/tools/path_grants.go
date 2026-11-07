package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PathGrants records directories outside the workspace that the user has
// let the agent use, per session, for the life of the process. A tool that
// hits a path outside its confinement asks once per directory through the
// UI bridge; non-interactive runs can't answer, so they stay confined.
type PathGrants struct {
	mu     sync.Mutex
	grants map[string][]pathGrant // keyed by session ID

	// askMu serialises prompts so parallel tool calls touching the same
	// directory produce one question, not several.
	askMu sync.Mutex
}

type pathGrant struct {
	dir    string // symlink-resolved directory
	write  bool   // write grants also cover reads
	denied bool   // the user said no; don't ask again this session
}

// NewPathGrants returns an empty grant store.
func NewPathGrants() *PathGrants {
	return &PathGrants{grants: make(map[string][]pathGrant)}
}

// allowed reports whether an existing grant covers target for the session.
func (g *PathGrants) allowed(session, target string, write bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, gr := range g.grants[session] {
		if !gr.denied && (gr.write || !write) && isConfined(gr.dir, target) {
			return true
		}
	}
	return false
}

// refused reports whether the user already declined access covering target
// for the session, at the same or a broader level than requested.
func (g *PathGrants) refused(session, target string, write bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, gr := range g.grants[session] {
		if gr.denied && (!gr.write || write) && isConfined(gr.dir, target) {
			return true
		}
	}
	return false
}

func (g *PathGrants) add(session, dir string, write, denied bool) {
	if resolved, err := resolveSymlinks(dir); err == nil {
		dir = resolved
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants[session] = append(g.grants[session], pathGrant{dir: dir, write: write, denied: denied})
}

// Permit reports whether a tool may touch target despite it being outside
// the workspace, asking the user when no grant covers it yet. A nil store
// or bridge, a non-interactive bridge, a cancelled prompt and a "no" all
// mean the path stays blocked.
func (g *PathGrants) Permit(ctx context.Context, ui UIBridge, target string, write bool) bool {
	if g == nil || ui == nil {
		return false
	}
	session := ui.SessionID()
	if g.allowed(session, target, write) {
		return true
	}

	g.askMu.Lock()
	defer g.askMu.Unlock()
	// Another call may have been granted while this one waited.
	if g.allowed(session, target, write) {
		return true
	}
	if g.refused(session, target, write) {
		return false
	}

	dir := grantDir(target)
	verb, scope := "read", "reading"
	if write {
		verb, scope = "change", "reading and editing"
	}
	ok, err := ui.Confirm(ctx,
		"Allow access outside the project?",
		fmt.Sprintf("The agent wants to %s %s.\n\nAllow %s files under %s for the rest of this session?", verb, target, scope, dir),
	)
	if err != nil {
		// Unsupported or cancelled: nothing to remember, the next call may ask.
		return false
	}
	g.add(session, dir, write, !ok)
	return ok
}

// readable reports whether a read-only tool may touch target: inside its
// read confinement, or covered by a grant (asking if needed).
func (g *PathGrants) readable(ctx context.Context, ui UIBridge, cwd, target string) bool {
	return isReadConfined(cwd, target) || g.Permit(ctx, ui, target, false)
}

// writable is readable's counterpart for the mutating tools, which are
// confined to the workspace alone unless the user grants write access.
func (g *PathGrants) writable(ctx context.Context, ui UIBridge, cwd, target string) bool {
	return isConfined(cwd, target) || g.Permit(ctx, ui, target, true)
}

// grantDir is the directory a grant for target covers: target itself when
// it is a directory, otherwise the directory that contains it.
func grantDir(target string) string {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return filepath.Clean(target)
	}
	return filepath.Dir(filepath.Clean(target))
}
