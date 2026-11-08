package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

func TestParseAllowedTools(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \t\n ", nil},
		{"single", "read", []string{"read"}},
		{"comma separated", "read,write,edit", []string{"read", "write", "edit"}},
		{"space separated", "read write edit", []string{"read", "write", "edit"}},
		{"mixed separators", "read, write\tedit\ngrep", []string{"read", "write", "edit", "grep"}},
		{"empty entries between commas", "read,,write", []string{"read", "write"}},
		{"leading/trailing whitespace", "  read, write  ", []string{"read", "write"}},
		{"commas only", ",,,", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAllowedTools(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseAllowedTools(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFindSkill(t *testing.T) {
	catalog := []*skills.Skill{
		{Name: "code-review"},
		{Name: "Deploy"},
		nil, // nil entries must be skipped, not panic
	}

	tests := []struct {
		name  string
		names []*skills.Skill
		query string
		want  string // empty means no match
	}{
		{"exact match", catalog, "code-review", "code-review"},
		{"case-insensitive match", catalog, "Code-Review", "code-review"},
		{"case-insensitive upper", catalog, "DEPLOY", "Deploy"},
		{"prefix is not a match", catalog, "code", ""},
		{"suffix is not a match", catalog, "review", ""},
		{"partial is not a match", catalog, "deploy-something", ""},
		{"unknown name", catalog, "nonexistent", ""},
		{"empty query", catalog, "", ""},
		{"empty catalog", nil, "code-review", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSkill(tt.names, tt.query)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("findSkill(%v, %q) = %v, want nil", tt.names, tt.query, got.Name)
				}
				return
			}
			if got == nil || got.Name != tt.want {
				t.Fatalf("findSkill(%v, %q) = %v, want %q", tt.names, tt.query, got, tt.want)
			}
		})
	}
}

// writeSkill writes a SKILL.md with the given frontmatter fields and body into
// dir/name/SKILL.md, returning the skill directory.
func writeSkill(t *testing.T, root, name, extraFrontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test skill " + name + "\n" + extraFrontmatter + "---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newTestSkillManager(t *testing.T, skillsRoot string) (*eventbus.Bus, *skills.Manager) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	mgr := skills.NewManager(bus)
	t.Cleanup(mgr.Close)
	workDir := t.TempDir()
	if _, err := mgr.Refresh(skills.DiscoveryConfig{WorkingDir: workDir, ExtraPaths: []string{skillsRoot}}); err != nil {
		t.Fatalf("skill refresh: %v", err)
	}
	return bus, mgr
}

func TestRegisterSkillTool(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "code-review", "allowed-tools: read, grep\n", "# Code Review\nReview the diff carefully.")
	writeSkill(t, root, "deploy", "", "# Deploy\nShip it.")

	_, mgr := newTestSkillManager(t, root)
	tracker := skills.NewTracker()
	reg := NewRegistry()

	var gotAllowed []string
	RegisterSkillTool(reg, mgr, tracker, func(tools []string) { gotAllowed = tools })

	tool, ok := reg.Get("skill")
	if !ok {
		t.Fatal("skill tool was not registered")
	}
	if tool.Schema.Name != "skill" {
		t.Fatalf("unexpected tool name %q", tool.Schema.Name)
	}

	exec := func(raw string) Result {
		t.Helper()
		res, err := tool.Execute(context.Background(), json.RawMessage(raw), nil)
		if err != nil {
			t.Fatalf("execute %s: %v", raw, err)
		}
		return res
	}

	// Activating a skill returns its instructions, records activation, and
	// applies its allowed-tools restriction. Matching is case-insensitive.
	res := exec(`{"name":"Code-Review"}`)
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.Content)
	}
	if res.Content != "# Code Review\nReview the diff carefully." {
		t.Fatalf("unexpected instructions: %q", res.Content)
	}
	if !reflect.DeepEqual(gotAllowed, []string{"read", "grep"}) {
		t.Fatalf("allowed tools = %v, want [read grep]", gotAllowed)
	}
	if act := tracker.Activated(); !reflect.DeepEqual(act, []string{"code-review"}) {
		t.Fatalf("activated = %v, want [code-review]", act)
	}

	// A skill without allowed-tools clears any previous restriction.
	res = exec(`{"name":"deploy"}`)
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.Content)
	}
	if gotAllowed != nil {
		t.Fatalf("allowed tools = %v, want nil (cleared)", gotAllowed)
	}

	// Unknown skill is an in-band error, not a panic.
	res = exec(`{"name":"nope"}`)
	if !res.IsError {
		t.Fatal("expected error for unknown skill")
	}

	// Empty name is rejected before lookup.
	res = exec(`{"name":"  "}`)
	if !res.IsError {
		t.Fatal("expected error for empty name")
	}

	// Invalid JSON is rejected.
	res = exec(`{invalid`)
	if !res.IsError {
		t.Fatal("expected error for invalid JSON")
	}
}

// A nil tracker and nil callback must not panic - the tool degrades to
// returning instructions only.
func TestRegisterSkillTool_NilTrackerAndCallback(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "solo", "", "do the thing")

	_, mgr := newTestSkillManager(t, root)
	reg := NewRegistry()
	RegisterSkillTool(reg, mgr, nil, nil)

	tool, ok := reg.Get("skill")
	if !ok {
		t.Fatal("skill tool was not registered")
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"solo"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError || res.Content != "do the thing" {
		t.Fatalf("unexpected result: %#v", res)
	}
}
