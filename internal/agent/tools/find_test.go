package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestFindFallback(t *testing.T) {
	// Create a temporary directory tree.
	tmp := t.TempDir()
	createTestFile(t, tmp, "foo.go", "package main")
	createTestFile(t, tmp, "bar.go", "package bar")
	createTestFile(t, tmp, "README.md", "# readme")
	createTestFile(t, tmp, "subdir/baz.go", "package baz")
	createTestFile(t, tmp, "subdir/nested/deep.txt", "deep")
	createTestFile(t, tmp, "vendor/mod.go", "package mod")
	os.MkdirAll(filepath.Join(tmp, "hiddendir"), 0o755)
	createTestFile(t, tmp, "hiddendir/visible.txt", "text")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cases := []struct {
		name      string
		params    FindParams
		want      []string
		wantEmpty bool
	}{
		{
			name:   "glob pattern *.go (matches all depths)",
			params: FindParams{Path: tmp, Pattern: "*.go", Type: "file"},
			want:   []string{"bar.go", "foo.go", "subdir/baz.go", "vendor/mod.go"},
		},
		{
			name:   "match directories (nested shown)",
			params: FindParams{Path: tmp, Type: "directory"},
			want:   []string{"hiddendir", "subdir", "subdir/nested", "vendor"},
		},
		{
			name:   "max depth 1",
			params: FindParams{Path: tmp, Type: "file", MaxDepth: 1},
			want:   []string{"bar.go", "foo.go", "README.md"},
		},
		{
			name:   "max depth 0 unlimited",
			params: FindParams{Path: tmp, Type: "file", MaxDepth: 0},
			want: []string{
				"bar.go", "foo.go", "README.md",
				"hiddendir/visible.txt", "subdir/baz.go",
				"subdir/nested/deep.txt", "vendor/mod.go",
			},
		},
		{
			name:   "exclude vendor",
			params: FindParams{Path: tmp, Type: "file", Exclude: "vendor"},
			want: []string{
				"bar.go", "foo.go", "README.md",
				"hiddendir/visible.txt", "subdir/baz.go",
				"subdir/nested/deep.txt",
			},
		},
		{
			name:   "exclude by glob",
			params: FindParams{Path: tmp, Type: "file", Exclude: "*.md"},
			want: []string{
				"bar.go", "foo.go",
				"hiddendir/visible.txt", "subdir/baz.go",
				"subdir/nested/deep.txt", "vendor/mod.go",
			},
		},
		{
			name:      "no matches",
			params:    FindParams{Path: tmp, Pattern: "nonexistent*", Type: "file"},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runFindGoFallback(ctx, tmp, tmp, tc.params)
			if err != nil {
				t.Fatalf("runFindGoFallback returned error: %v", err)
			}
			if tc.wantEmpty {
				if res.Content != "no matches found" {
					t.Fatalf("expected empty, got:\n%s", res.Content)
				}
				return
			}
			gotLines := strings.Split(strings.TrimSpace(res.Content), "\n")
			if len(gotLines) == 1 && gotLines[0] == "" {
				gotLines = []string{}
			}
			sort.Strings(gotLines)
			sort.Strings(tc.want)
			if len(gotLines) != len(tc.want) {
				t.Fatalf("got %d results, want %d\ngot: %v\nwant: %v", len(gotLines), len(tc.want), gotLines, tc.want)
			}
			for i := range tc.want {
				if gotLines[i] != tc.want[i] {
					t.Fatalf("result mismatch at %d: got %q, want %q", i, gotLines[i], tc.want[i])
				}
			}
		})
	}
}

func TestFindFallback_NonExistentPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, _ := runFindGoFallback(ctx, "/nonexistent/path", "/nonexistent/path", FindParams{})
	if !res.IsError {
		t.Fatal("expected error for non-existent path")
	}
}

func TestFindFallback_NotADirectory(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, _ := runFindGoFallback(ctx, tmp, f, FindParams{})
	if !res.IsError {
		t.Fatal("expected error when path is a file")
	}
}

func TestBuildFindArgs_POSIXExclude(t *testing.T) {
	args := buildFindArgs("find", FindParams{Exclude: "node_modules"}, "/tmp/x")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-not ( -path */node_modules -o -path */node_modules/* )") {
		t.Fatalf("exclude must match both the entry and its contents, got: %v", args)
	}

	// Glob metacharacters in the exclude stay inside the single -path
	// argument; they cannot become separate find expression tokens.
	args = buildFindArgs("find", FindParams{Exclude: "*.test.go"}, "/tmp/x")
	for i, a := range args {
		if strings.HasPrefix(a, "-") && a != "-not" && a != "(" && a != "-path" && a != "-o" && a != ")" {
			t.Fatalf("exclude leaked into argument %d (%q) of %v", i, a, args)
		}
	}
	if !strings.Contains(strings.Join(args, " "), "-path */*.test.go ") {
		t.Fatalf("expected glob exclude preserved in -path, got: %v", args)
	}
}

// TestFindPOSIXExcludeSemantics runs real find(1) against a temp tree when
// available, proving the POSIX fallback excludes both directories and files.
func TestFindPOSIXExcludeSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX find not available on Windows")
	}
	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find not in PATH")
	}

	tmp := t.TempDir()
	createTestFile(t, tmp, "main.go", "package main")
	createTestFile(t, tmp, "main.test.go", "package main")
	createTestFile(t, tmp, "node_modules/dep/index.js", "x")
	createTestFile(t, tmp, "sub/node_modules/dep/index.js", "x")

	run := func(p FindParams) []string {
		args := buildFindArgs("find", p, tmp)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "find", args...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("find %v: %v", args, err)
		}
		var got []string
		for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			rel, err := filepath.Rel(tmp, line)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, filepath.ToSlash(rel))
		}
		sort.Strings(got)
		return got
	}

	// Directory exclude prunes at any depth, including the dir entry itself.
	got := run(FindParams{Type: "file", Exclude: "node_modules"})
	want := []string{"main.go", "main.test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exclude node_modules: got %v, want %v", got, want)
	}

	// Glob file exclude works (the old */%s/* wrapper could never match a file).
	got = run(FindParams{Type: "file", Exclude: "*.test.go"})
	want = []string{"main.go", "node_modules/dep/index.js", "sub/node_modules/dep/index.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exclude *.test.go: got %v, want %v", got, want)
	}
}

func createTestFile(t *testing.T, base, relPath, content string) {
	t.Helper()
	full := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
