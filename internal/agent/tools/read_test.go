package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execRead(t *testing.T, cwd string, params string) Result {
	t.Helper()
	tool := NewReadTool(cwd, nil)
	res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

func TestReadTool_RawContentNoGutter(t *testing.T) {
	tmp := t.TempDir()
	writeReadTestFile(t, tmp, "f.txt", "alpha\nbeta\ngamma\n")

	res := execRead(t, tmp, `{"path": "f.txt"}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if strings.Contains(res.Content, "│") {
		t.Fatalf("expected raw content without line-number gutter, got:\n%s", res.Content)
	}
	if !strings.HasPrefix(res.Content, "alpha\nbeta\ngamma") {
		t.Fatalf("unexpected content:\n%s", res.Content)
	}
}

func TestReadToolAcceptsFileAlias(t *testing.T) {
	tmp := t.TempDir()
	writeReadTestFile(t, tmp, "f.txt", "alpha\nbeta\n")

	res := execRead(t, tmp, `{"file":"f.txt"}`)
	if res.IsError || !strings.HasPrefix(res.Content, "alpha\nbeta") {
		t.Fatalf("file alias result = %#v", res)
	}
}

func TestReadToolDefaultsToBoundedOutput(t *testing.T) {
	tmp := t.TempDir()
	lines := make([]string, DefaultReadLines+10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	writeReadTestFile(t, tmp, "f.txt", strings.Join(lines, "\n"))

	res := execRead(t, tmp, `{"path":"f.txt"}`)
	if res.IsError || !strings.Contains(res.Content, fmt.Sprintf("Use offset=%d to continue", DefaultReadLines+1)) {
		t.Fatalf("bounded read result = %s", res.Content)
	}
}

func TestReadToolReadsGoSymbol(t *testing.T) {
	tmp := t.TempDir()
	writeReadTestFile(t, tmp, "f.go", "package sample\n\nfunc first() {}\n\nfunc target() {\n\tprintln(\"yes\")\n}\n\nfunc last() {}\n")

	res := execRead(t, tmp, `{"path":"f.go","symbol":"target"}`)
	if res.IsError || !strings.Contains(res.Content, "func target()") || strings.Contains(res.Content, "func first()") || strings.Contains(res.Content, "func last()") {
		t.Fatalf("symbol read result = %s", res.Content)
	}
}

func TestReadTool_UserLimitContinuationNotice(t *testing.T) {
	tmp := t.TempDir()
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeReadTestFile(t, tmp, "f.txt", strings.Join(lines, "\n"))

	res := execRead(t, tmp, `{"path": "f.txt", "offset": 10, "limit": 5}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if !strings.HasPrefix(res.Content, "line 10") {
		t.Fatalf("expected content to start at line 10, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Use offset=15 to continue") {
		t.Fatalf("expected continuation notice with offset=15, got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "more lines in file") {
		t.Fatalf("expected remaining-lines notice, got:\n%s", res.Content)
	}
}

func TestReadTool_TruncationContinuationNotice(t *testing.T) {
	tmp := t.TempDir()
	var lines []string
	for i := 1; i <= DefaultMaxLines+500; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeReadTestFile(t, tmp, "f.txt", strings.Join(lines, "\n"))

	res := execRead(t, tmp, `{"path": "f.txt", "full": true}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	notice := fmt.Sprintf("[showing lines 1-%d of %d. Use offset=%d to continue.]", DefaultMaxLines, DefaultMaxLines+500, DefaultMaxLines+1)
	if !strings.Contains(res.Content, notice) {
		t.Fatalf("expected notice %q, got tail:\n%s", notice, res.Content[len(res.Content)-200:])
	}
}

func TestReadTool_FirstLineExceedsByteLimit(t *testing.T) {
	tmp := t.TempDir()
	writeReadTestFile(t, tmp, "big.min.js", strings.Repeat("x", DefaultMaxBytes+100))

	res := execRead(t, tmp, `{"path": "big.min.js"}`)
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "Use shell: sed -n '1p'") {
		t.Fatalf("expected shell fallback hint, got:\n%s", res.Content)
	}
}

func writeReadTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
