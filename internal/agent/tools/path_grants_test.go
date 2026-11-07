package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// grantBridge answers Confirm with a fixed reply and counts the prompts.
type grantBridge struct {
	NonInteractiveBridge
	mu      sync.Mutex
	answer  bool
	session string
	asked   int
	last    string
}

func (b *grantBridge) Confirm(_ context.Context, _, description string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asked++
	b.last = description
	return b.answer, nil
}

func (b *grantBridge) SessionID() string { return b.session }

// outsideDir makes a workspace and a sibling directory outside it holding
// one file, returning the workspace, the outside dir and the file.
func outsideDir(t *testing.T) (cwd, outside, file string) {
	t.Helper()
	root := t.TempDir()
	cwd = filepath.Join(root, "project")
	outside = filepath.Join(root, "elsewhere")
	for _, d := range []string{cwd, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file = filepath.Join(outside, "notes.txt")
	if err := os.WriteFile(file, []byte("outside content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd, outside, file
}

func runTool(t *testing.T, tool Tool, ui UIBridge, params any) Result {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Execute(context.Background(), raw, ui)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestReadOutsideWorkspaceAsksOncePerDirectory(t *testing.T) {
	cwd, outside, file := outsideDir(t)
	other := filepath.Join(outside, "more.txt")
	if err := os.WriteFile(other, []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui := &grantBridge{answer: true, session: "s1"}
	tool := newReadTool(cwd, NewReadTracker(), NewPathGrants())

	res := runTool(t, tool, ui, map[string]any{"path": file})
	if res.IsError || !strings.Contains(res.Content, "outside content") {
		t.Fatalf("granted read failed: %+v", res)
	}
	if !strings.Contains(ui.last, outside) {
		t.Fatalf("prompt should name the directory being granted, got %q", ui.last)
	}

	res = runTool(t, tool, ui, map[string]any{"path": other})
	if res.IsError {
		t.Fatalf("second file in a granted directory failed: %+v", res)
	}
	if ui.asked != 1 {
		t.Fatalf("expected one prompt for the directory, got %d", ui.asked)
	}
}

func TestReadOutsideWorkspaceDenied(t *testing.T) {
	cwd, _, file := outsideDir(t)
	ui := &grantBridge{answer: false, session: "s1"}
	tool := newReadTool(cwd, NewReadTracker(), NewPathGrants())

	res := runTool(t, tool, ui, map[string]any{"path": file})
	if !res.IsError || res.ErrorKind != "sandbox_escape" {
		t.Fatalf("denied read should stay a sandbox escape, got %+v", res)
	}
}

func TestNonInteractiveStaysConfined(t *testing.T) {
	cwd, _, file := outsideDir(t)
	tool := newReadTool(cwd, NewReadTracker(), NewPathGrants())

	res := runTool(t, tool, NonInteractiveBridge{}, map[string]any{"path": file})
	if !res.IsError {
		t.Fatalf("non-interactive read outside the workspace must fail, got %+v", res)
	}
}

func TestExportedConstructorsNeverPrompt(t *testing.T) {
	cwd, _, file := outsideDir(t)
	ui := &grantBridge{answer: true}
	res := runTool(t, NewReadTool(cwd, NewReadTracker()), ui, map[string]any{"path": file})
	if !res.IsError || ui.asked != 0 {
		t.Fatalf("a tool without a grant store must not prompt: asked=%d res=%+v", ui.asked, res)
	}
}

func TestReadGrantDoesNotCoverWrites(t *testing.T) {
	cwd, _, file := outsideDir(t)
	grants := NewPathGrants()
	rt := NewReadTracker()
	ui := &grantBridge{answer: true, session: "s1"}

	runTool(t, newReadTool(cwd, rt, grants), ui, map[string]any{"path": file})
	ui.answer = false
	res := runTool(t, newWriteTool(cwd, nil, rt, grants), ui, map[string]any{"path": file, "content": "x"})
	if !res.IsError {
		t.Fatalf("a read grant must not allow writes, got %+v", res)
	}
	if ui.asked != 2 {
		t.Fatalf("expected a separate prompt for write access, got %d prompts", ui.asked)
	}
}

func TestWriteGrantCoversReads(t *testing.T) {
	_, outside, file := outsideDir(t)
	g := NewPathGrants()
	ui := &grantBridge{answer: true, session: "s1"}
	if !g.Permit(context.Background(), ui, file, true) {
		t.Fatal("write grant refused")
	}
	if !g.Permit(context.Background(), ui, filepath.Join(outside, "new.txt"), false) {
		t.Fatal("write grant should cover reads in the same directory")
	}
	if ui.asked != 1 {
		t.Fatalf("expected one prompt, got %d", ui.asked)
	}
}

func TestGrantsAreScopedToSession(t *testing.T) {
	_, _, file := outsideDir(t)
	g := NewPathGrants()
	g.Permit(context.Background(), &grantBridge{answer: true, session: "s1"}, file, false)

	other := &grantBridge{answer: false, session: "s2"}
	if g.Permit(context.Background(), other, file, false) {
		t.Fatal("a grant in one session must not leak into another")
	}
}

func TestGrantDoesNotCoverSiblingDirectories(t *testing.T) {
	_, outside, file := outsideDir(t)
	sibling := filepath.Join(filepath.Dir(outside), "elsewhere-too")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	g := NewPathGrants()
	ui := &grantBridge{answer: true, session: "s1"}
	g.Permit(context.Background(), ui, file, false)

	ui.answer = false
	if g.Permit(context.Background(), ui, filepath.Join(sibling, "x.txt"), false) {
		t.Fatal("a grant must not cover a sibling directory with a shared prefix")
	}
}

func TestParallelRequestsPromptOnce(t *testing.T) {
	_, _, file := outsideDir(t)
	g := NewPathGrants()
	ui := &grantBridge{answer: true, session: "s1"}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { g.Permit(context.Background(), ui, file, false) })
	}
	wg.Wait()
	if ui.asked != 1 {
		t.Fatalf("parallel calls for one directory should prompt once, got %d", ui.asked)
	}
}

func TestGrepAndFindUseGrants(t *testing.T) {
	cwd, outside, _ := outsideDir(t)
	grants := NewPathGrants()
	ui := &grantBridge{answer: true, session: "s1"}

	res := runTool(t, newFindTool(cwd, grants), ui, map[string]any{"path": outside, "pattern": "*.txt"})
	if res.IsError {
		t.Fatalf("granted find failed: %+v", res)
	}
	res = runTool(t, newGrepTool(cwd, grants), ui, map[string]any{"path": outside, "pattern": "outside"})
	if res.IsError {
		t.Fatalf("granted grep failed: %+v", res)
	}
	if ui.asked != 1 {
		t.Fatalf("find and grep on one directory should share a grant, got %d prompts", ui.asked)
	}
}

func TestDeniedDirectoryIsNotAskedAgain(t *testing.T) {
	_, outside, file := outsideDir(t)
	g := NewPathGrants()
	ui := &grantBridge{answer: false, session: "s1"}
	ctx := context.Background()

	if g.Permit(ctx, ui, file, false) {
		t.Fatal("denied request was permitted")
	}
	if g.Permit(ctx, ui, filepath.Join(outside, "other.txt"), false) {
		t.Fatal("a refused directory must stay refused")
	}
	if g.Permit(ctx, ui, file, true) {
		t.Fatal("refusing reads must also refuse writes")
	}
	if ui.asked != 1 {
		t.Fatalf("expected one prompt after a refusal, got %d", ui.asked)
	}
}

func TestWriteRefusalStillAllowsAskingForReads(t *testing.T) {
	_, _, file := outsideDir(t)
	g := NewPathGrants()
	ui := &grantBridge{answer: false, session: "s1"}
	ctx := context.Background()

	g.Permit(ctx, ui, file, true)
	ui.answer = true
	if !g.Permit(ctx, ui, file, false) {
		t.Fatal("refusing writes should not stop the user granting reads")
	}
	if ui.asked != 2 {
		t.Fatalf("expected a second prompt for read access, got %d", ui.asked)
	}
}

// cancelBridge reports a cancelled prompt.
type cancelBridge struct{ grantBridge }

func (b *cancelBridge) Confirm(context.Context, string, string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asked++
	return false, ErrInteractiveCanceled
}

func TestCancelledPromptIsNotRemembered(t *testing.T) {
	_, _, file := outsideDir(t)
	g := NewPathGrants()
	ui := &cancelBridge{grantBridge{session: "s1"}}
	g.Permit(context.Background(), ui, file, false)
	g.Permit(context.Background(), ui, file, false)
	if ui.asked != 2 {
		t.Fatalf("a cancelled prompt should be asked again, got %d prompts", ui.asked)
	}
}
