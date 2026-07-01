package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrepFallback(t *testing.T) {
	// Create a temporary directory with test files.
	tmp := t.TempDir()
	createGrepTestFile(t, tmp, "alpha.go", "package alpha\nfunc Hello() {}\n")
	createGrepTestFile(t, tmp, "beta.go", "package beta\nfunc World() {}\nfunc hello() {}\n")
	createGrepTestFile(t, tmp, "gamma.txt", "Hello world\n")
	createGrepTestFile(t, tmp, "vendor/mod.go", "package vendor\nfunc Hello() {}\n")
	createGrepTestFile(t, tmp, "subdir/nested.go", "package nested\nfunc HelloThere() {}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cases := []struct {
		name      string
		params    GrepParams
		want      []string
		wantEmpty bool
	}{
		{
			name:   "literal match in file",
			params: GrepParams{Pattern: "Hello", Path: filepath.Join(tmp, "alpha.go")},
			want:   []string{"alpha.go:2:func Hello() {}"},
		},
		{
			name:   "literal match in directory",
			params: GrepParams{Pattern: "Hello", Path: tmp},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"beta.go:3:func hello() {}",
				"gamma.txt:1:Hello world",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "case sensitive literal",
			params: GrepParams{Pattern: "Hello", Path: tmp, CaseSensitive: true},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"gamma.txt:1:Hello world",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "regex match",
			params: GrepParams{Pattern: "Hello.*", Path: tmp, IsRegex: true},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"gamma.txt:1:Hello world",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "include filter",
			params: GrepParams{Pattern: "Hello", Path: tmp, Include: "*.go"},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"beta.go:3:func hello() {}",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "context lines",
			params: GrepParams{Pattern: "Hello", Path: filepath.Join(tmp, "alpha.go"), ContextBefore: 1, ContextAfter: 1},
			want: []string{
				"alpha.go:1:package alpha",
				"alpha.go:2:func Hello() {}",
				"alpha.go:3:", // trailing newline yields empty third line
			},
		},
		{
			name:      "no matches",
			params:    GrepParams{Pattern: "nonexistentXYZ", Path: tmp},
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := grepFallback(ctx, tc.params, tc.params.Path, tmp)
			if err != nil {
				t.Fatalf("grepFallback error: %v", err)
			}
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got:\n%s", got)
				}
				return
			}
			gotLines := strings.Split(strings.TrimSpace(got), "\n")
			if len(gotLines) == 1 && gotLines[0] == "" {
				gotLines = []string{}
			}
			if len(gotLines) != len(tc.want) {
				t.Fatalf("got %d results, want %d\ngot:\n%s\nwant: %v", len(gotLines), len(tc.want), got, tc.want)
			}
			for i := range tc.want {
				if gotLines[i] != tc.want[i] {
					t.Fatalf("result mismatch at %d: got %q, want %q", i, gotLines[i], tc.want[i])
				}
			}
		})
	}
}

func TestGrepFallback_ContextDedup(t *testing.T) {
	tmp := t.TempDir()
	content := "line1\nHello\nline3\nHello\nline5\n"
	createGrepTestFile(t, tmp, "test.txt", content)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	params := GrepParams{Pattern: "Hello", Path: tmp, ContextBefore: 1, ContextAfter: 1}
	got, err := grepFallback(ctx, params, tmp, tmp)
	if err != nil {
		t.Fatalf("grepFallback error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(got), "\n")
	// Expected:
	// line1 (context for first Hello)
	// Hello (match 1)
	// line3 (context after first Hello + context before second Hello — dedup)
	// Hello (match 2)
	// line5 (context after second Hello)
	// Total unique lines: 5
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d:\n%s", len(lines), got)
	}
}

func TestGrepFallback_NonExistentPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := grepFallback(ctx, GrepParams{Pattern: "foo"}, "/nonexistent/path", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func createGrepTestFile(t *testing.T, base, relPath, content string) {
	t.Helper()
	full := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
