package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withGoModCache points the module-cache lookup at a temp dir for the duration
// of a test, so these tests never depend on the host's real GOMODCACHE.
func withGoModCache(t *testing.T, dir string) {
	t.Helper()
	prev := goModCacheDir
	goModCacheDir = func() string { return dir }
	t.Cleanup(func() { goModCacheDir = prev })
}

func TestReadAllowsGoModCache(t *testing.T) {
	cwd := t.TempDir()
	cache := t.TempDir()
	withGoModCache(t, cache)

	dep := filepath.Join(cache, "example.com", "dep@v1.0.0")
	if err := os.MkdirAll(dep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "dep.go"), []byte("package dep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := execRead(t, cwd, `{"path":`+quote(filepath.Join(dep, "dep.go"))+`}`)
	if res.IsError {
		t.Fatalf("expected read of a module-cache file to succeed, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "package dep") {
		t.Fatalf("unexpected content: %s", res.Content)
	}
}

func TestGrepAllowsGoModCache(t *testing.T) {
	cwd := t.TempDir()
	cache := t.TempDir()
	withGoModCache(t, cache)

	if err := os.WriteFile(filepath.Join(cache, "dep.go"), []byte("package dep\nfunc Needle() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewGrepTool(cwd, nil)
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"pattern":"Needle","path":`+quote(cache)+`}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected grep of the module cache to succeed, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "Needle") {
		t.Fatalf("unexpected content: %s", res.Content)
	}
}

// The module cache is a read-only escape hatch. Mutating tools must still be
// confined to the workspace, or the agent could corrupt shared dependencies.
func TestWriteRejectsGoModCache(t *testing.T) {
	cwd := t.TempDir()
	cache := t.TempDir()
	withGoModCache(t, cache)

	tool := NewWriteTool(cwd, nil, NewReadTracker())
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"path":`+quote(filepath.Join(cache, "evil.go"))+`,"content":"package evil\n"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
		t.Fatalf("expected write to the module cache to be rejected, got: %#v", res)
	}
}

func TestEditRejectsGoModCache(t *testing.T) {
	cwd := t.TempDir()
	cache := t.TempDir()
	withGoModCache(t, cache)

	target := filepath.Join(cache, "dep.go")
	if err := os.WriteFile(target, []byte("package dep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := NewReadTracker()
	rt.MarkRead(cwd, target)
	tool := NewEditTool(cwd, nil, rt)
	res, err := tool.Execute(context.Background(), json.RawMessage(
		`{"path":`+quote(target)+`,"edits":[{"old_text":"package dep","new_text":"package hacked"}]}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
		t.Fatalf("expected edit of the module cache to be rejected, got: %#v", res)
	}
}

// A path in neither the workspace nor the module cache stays rejected.
func TestReadRejectsUnrelatedAbsolutePath(t *testing.T) {
	cwd := t.TempDir()
	cache := t.TempDir()
	other := t.TempDir()
	withGoModCache(t, cache)

	target := filepath.Join(other, "secret.txt")
	if err := os.WriteFile(target, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := execRead(t, cwd, `{"path":`+quote(target)+`}`)
	if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
		t.Fatalf("expected an unrelated absolute path to be rejected, got: %#v", res)
	}
}

func TestResolvePath(t *testing.T) {
	cwd := filepath.Clean("/workspace/project")

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"relative simple", "file.txt", filepath.Join(cwd, "file.txt")},
		{"relative subpath", "sub/dir/file.txt", filepath.Join(cwd, "sub", "dir", "file.txt")},
		{"relative with dot", "./file.txt", filepath.Join(cwd, "file.txt")},
		{"relative with parent", "sub/../file.txt", filepath.Join(cwd, "file.txt")},
		{"absolute path", "/etc/passwd", filepath.Clean("/etc/passwd")},
		{"at prefixed relative", "@file.txt", filepath.Join(cwd, "file.txt")},
		{"at prefixed absolute", "@/etc/passwd", filepath.Clean("/etc/passwd")},
		{"empty path", "", cwd},
		{"dot path", ".", cwd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePath(cwd, tt.path)
			if got != tt.expected {
				t.Fatalf("resolvePath(%q, %q) = %q, want %q", cwd, tt.path, got, tt.expected)
			}
		})
	}
}

func TestIsConfined_Basic(t *testing.T) {
	base := t.TempDir()
	subDir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fileInside := filepath.Join(subDir, "file.txt")
	if err := os.WriteFile(fileInside, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	otherDir := t.TempDir()

	tests := []struct {
		name     string
		base     string
		target   string
		expected bool
	}{
		{"exact base", base, base, true},
		{"sub directory", base, subDir, true},
		{"file inside", base, fileInside, true},
		{"nonexistent file inside", base, filepath.Join(base, "new.txt"), true},
		{"nonexistent nested file inside", base, filepath.Join(base, "a", "b", "c.txt"), true},
		{"parent via dotdot", base, filepath.Join(base, ".."), false},
		{"outside sibling", base, filepath.Join(base, "..", filepath.Base(otherDir)), false},
		{"unrelated absolute", base, otherDir, false},
		{"empty base allows all", "", otherDir, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfined(tt.base, tt.target)
			if got != tt.expected {
				t.Fatalf("isConfined(%q, %q) = %v, want %v", tt.base, tt.target, got, tt.expected)
			}
		})
	}
}

func TestIsConfined_Symlinks(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()

	insideFile := filepath.Join(base, "inside.txt")
	if err := os.WriteFile(insideFile, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink pointing to outside file
	linkToOutsideFile := filepath.Join(base, "link_to_outside_file")
	if err := os.Symlink(outsideFile, linkToOutsideFile); err != nil {
		t.Fatal(err)
	}

	// Symlink pointing to outside directory
	linkToOutsideDir := filepath.Join(base, "link_to_outside_dir")
	if err := os.Symlink(outside, linkToOutsideDir); err != nil {
		t.Fatal(err)
	}

	// Symlink pointing to inside file
	linkToInsideFile := filepath.Join(base, "link_to_inside_file")
	if err := os.Symlink(insideFile, linkToInsideFile); err != nil {
		t.Fatal(err)
	}

	// Symlink pointing to inside directory
	subDir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkToInsideDir := filepath.Join(base, "link_to_inside_dir")
	if err := os.Symlink(subDir, linkToInsideDir); err != nil {
		t.Fatal(err)
	}

	// Broken symlink pointing outside
	brokenOutside := filepath.Join(base, "broken_outside")
	if err := os.Symlink(filepath.Join(outside, "nonexistent.txt"), brokenOutside); err != nil {
		t.Fatal(err)
	}

	// Broken symlink pointing inside
	brokenInside := filepath.Join(base, "broken_inside")
	if err := os.Symlink(filepath.Join(base, "nonexistent.txt"), brokenInside); err != nil {
		t.Fatal(err)
	}

	// Symlink loop
	loopA := filepath.Join(base, "loopA")
	loopB := filepath.Join(base, "loopB")
	if err := os.Symlink(loopB, loopA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(loopA, loopB); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		target   string
		expected bool
	}{
		{"symlink to outside file rejected", linkToOutsideFile, false},
		{"symlink to outside dir rejected", linkToOutsideDir, false},
		{"existing file through outside symlink dir rejected", filepath.Join(linkToOutsideDir, "secret.txt"), false},
		{"nonexistent file through outside symlink dir rejected", filepath.Join(linkToOutsideDir, "new.txt"), false},
		{"symlink to inside file allowed", linkToInsideFile, true},
		{"symlink to inside dir allowed", linkToInsideDir, true},
		{"file through inside symlink dir allowed", filepath.Join(linkToInsideDir, "new.txt"), true},
		{"broken symlink to outside rejected", brokenOutside, false},
		{"broken symlink to inside allowed", brokenInside, true},
		{"symlink loop rejected", loopA, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfined(base, tt.target)
			if got != tt.expected {
				t.Fatalf("isConfined(%q, %q) = %v, want %v", base, tt.target, got, tt.expected)
			}
		})
	}
}

func TestToolsRejectSymlinkEscape(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()

	secretFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("super secret data\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkFile := filepath.Join(cwd, "link_file.txt")
	if err := os.Symlink(secretFile, linkFile); err != nil {
		t.Fatal(err)
	}

	linkDir := filepath.Join(cwd, "link_dir")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Fatal(err)
	}

	// 1. Read tool rejects reading through symlink pointing outside
	t.Run("read rejects symlink to outside file", func(t *testing.T) {
		res := execRead(t, cwd, `{"path":`+quote(linkFile)+`}`)
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected read of outside symlink to be rejected, got: %#v", res)
		}
	})

	t.Run("read rejects symlink dir to outside file", func(t *testing.T) {
		res := execRead(t, cwd, `{"path":`+quote(filepath.Join(linkDir, "secret.txt"))+`}`)
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected read through outside symlink dir to be rejected, got: %#v", res)
		}
	})

	// 2. Write tool rejects writing through symlink pointing outside
	t.Run("write rejects symlink to outside file", func(t *testing.T) {
		tool := NewWriteTool(cwd, nil, NewReadTracker())
		res, err := tool.Execute(context.Background(), json.RawMessage(
			`{"path":`+quote(linkFile)+`,"content":"overwrite\n"}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected write through outside symlink to be rejected, got: %#v", res)
		}
	})

	t.Run("write rejects creating new file through outside symlink dir", func(t *testing.T) {
		tool := NewWriteTool(cwd, nil, NewReadTracker())
		res, err := tool.Execute(context.Background(), json.RawMessage(
			`{"path":`+quote(filepath.Join(linkDir, "new_outside.txt"))+`,"content":"pwn\n"}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected write into outside symlink dir to be rejected, got: %#v", res)
		}
	})

	// 3. Edit tool rejects editing through symlink pointing outside
	t.Run("edit rejects symlink to outside file", func(t *testing.T) {
		rt := NewReadTracker()
		rt.MarkRead(cwd, linkFile)
		tool := NewEditTool(cwd, nil, rt)
		res, err := tool.Execute(context.Background(), json.RawMessage(
			`{"path":`+quote(linkFile)+`,"edits":[{"old_text":"super secret data","new_text":"modified"}]}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected edit through outside symlink to be rejected, got: %#v", res)
		}
	})

	// 4. Find tool rejects searching in outside symlink dir
	t.Run("find rejects outside symlink dir", func(t *testing.T) {
		tool := NewFindTool(cwd)
		res, err := tool.Execute(context.Background(), json.RawMessage(
			`{"path":`+quote(linkDir)+`}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected find in outside symlink dir to be rejected, got: %#v", res)
		}
	})

	// 5. Grep tool rejects searching in outside symlink dir
	t.Run("grep rejects outside symlink dir", func(t *testing.T) {
		tool := NewGrepTool(cwd, nil)
		res, err := tool.Execute(context.Background(), json.RawMessage(
			`{"pattern":"secret","path":`+quote(linkDir)+`}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content, "escapes working directory") {
			t.Fatalf("expected grep in outside symlink dir to be rejected, got: %#v", res)
		}
	})
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
