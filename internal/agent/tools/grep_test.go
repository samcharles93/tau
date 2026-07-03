package tools

import (
	"context"
	"fmt"
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
			// Smart case: an uppercase letter in the pattern makes the
			// search case-sensitive, matching ripgrep's --smart-case.
			name:   "match in directory (smart case)",
			params: GrepParams{Pattern: "Hello", Path: tmp},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"gamma.txt:1:Hello world",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "lowercase pattern matches both cases",
			params: GrepParams{Pattern: "hello", Path: tmp},
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
			name:   "regex match by default",
			params: GrepParams{Pattern: "Hello.*", Path: tmp},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"gamma.txt:1:Hello world",
				"subdir/nested.go:2:func HelloThere() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "regex alternation",
			params: GrepParams{Pattern: "World|world", Path: tmp},
			want: []string{
				"beta.go:2:func World() {}",
				"gamma.txt:1:Hello world",
			},
		},
		{
			name:   "literal match with regex metacharacters",
			params: GrepParams{Pattern: "Hello() {}", Path: tmp, Literal: true, CaseSensitive: true},
			want: []string{
				"alpha.go:2:func Hello() {}",
				"vendor/mod.go:2:func Hello() {}",
			},
		},
		{
			name:   "include filter",
			params: GrepParams{Pattern: "hello", Path: tmp, Include: "*.go"},
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
				"alpha.go-1-package alpha",
				"alpha.go:2:func Hello() {}",
				"alpha.go-3-", // trailing newline yields empty third line
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

func TestCapGrepOutput_MatchLimit(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("file.go:%d:match line", i))
	}
	output := strings.Join(lines, "\n")

	got := capGrepOutput(output, 5)

	if !strings.Contains(got, "[showing first 5 matches") {
		t.Fatalf("expected match-limit notice, got:\n%s", got)
	}
	matchCount := strings.Count(got, "match line")
	if matchCount != 5 {
		t.Fatalf("expected 5 match lines, got %d", matchCount)
	}
}

func TestCapGrepOutput_LongLines(t *testing.T) {
	long := "file.js:1:" + strings.Repeat("x", 2000)
	got := capGrepOutput(long, 100)

	if !strings.Contains(got, "... [truncated]") {
		t.Fatalf("expected per-line truncation marker, got:\n%s", got)
	}
	if !strings.Contains(got, "[some lines truncated to 500 chars]") {
		t.Fatalf("expected long-line notice, got:\n%s", got)
	}
	if len(strings.Split(got, "\n")[0]) > 600 {
		t.Fatalf("first line not capped: %d chars", len(strings.Split(got, "\n")[0]))
	}
}

func TestCapGrepOutput_ContextLinesNotCounted(t *testing.T) {
	// Context lines (dash separators) must not count towards the match limit.
	output := strings.Join([]string{
		"file.go-1-context before",
		"file.go:2:match one",
		"file.go-3-context after",
		"file.go:4:match two",
	}, "\n")

	got := capGrepOutput(output, 2)

	if strings.Contains(got, "[showing first") {
		t.Fatalf("limit notice should not fire for 2 matches with limit 2, got:\n%s", got)
	}
	if !strings.Contains(got, "match two") {
		t.Fatalf("expected both matches present, got:\n%s", got)
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
