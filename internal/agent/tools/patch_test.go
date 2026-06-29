package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyUnifiedDiff_SingleHunkReplace(t *testing.T) {
	original := "package main\n\nfunc main() {\n\tprintln(\"old\")\n}\n"
	diff := strings.TrimSpace(`
@@ -1,5 +1,5 @@
 package main

 func main() {
-	println("old")
+	println("new")
 }
`)

	got, stats, err := applyUnifiedDiff(original, diff)
	if err != nil {
		t.Fatalf("applyUnifiedDiff returned error: %v", err)
	}

	want := "package main\n\nfunc main() {\n\tprintln(\"new\")\n}\n"
	if got != want {
		t.Fatalf("patched content mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
	if stats.Hunks != 1 || stats.Added != 1 || stats.Removed != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyUnifiedDiff_MultipleHunks(t *testing.T) {
	original := "one\ntwo\nthree\nfour\nfive\n"
	diff := strings.TrimSpace(`
@@ -1,2 +1,2 @@
 one
-two
+TWO
@@ -4,2 +4,2 @@
 four
-five
+FIVE
`)

	got, stats, err := applyUnifiedDiff(original, diff)
	if err != nil {
		t.Fatalf("applyUnifiedDiff returned error: %v", err)
	}

	want := "one\nTWO\nthree\nfour\nFIVE\n"
	if got != want {
		t.Fatalf("patched content mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
	if stats.Hunks != 2 || stats.Added != 2 || stats.Removed != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyUnifiedDiff_ContextMismatch(t *testing.T) {
	original := "alpha\nbeta\ngamma\n"
	diff := strings.TrimSpace(`
@@ -1,3 +1,3 @@
 alpha
-BETA
+delta
 gamma
`)

	_, _, err := applyUnifiedDiff(original, diff)
	if err == nil {
		t.Fatal("expected applyUnifiedDiff to fail on context mismatch")
	}
	if !strings.Contains(err.Error(), "removal mismatch") {
		t.Fatalf("expected removal mismatch error, got: %v", err)
	}
}

func TestApplyUnifiedDiff_AdditionOnlyHunkAtTop(t *testing.T) {
	original := "beta\ngamma\n"
	diff := strings.TrimSpace(`
@@ -1,0 +1,1 @@
+alpha
`)

	got, stats, err := applyUnifiedDiff(original, diff)
	if err != nil {
		t.Fatalf("applyUnifiedDiff returned error: %v", err)
	}

	want := "alpha\nbeta\ngamma\n"
	if got != want {
		t.Fatalf("patched content mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
	if stats.Hunks != 1 || stats.Added != 1 || stats.Removed != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyUnifiedDiff_PreservesNoTrailingNewlineWhenRequested(t *testing.T) {
	original := "alpha\nbeta"
	diff := strings.TrimSpace(`
@@ -1,2 +1,2 @@
 alpha
-beta
+delta
\ No newline at end of file
`)

	got, _, err := applyUnifiedDiff(original, diff)
	if err != nil {
		t.Fatalf("applyUnifiedDiff returned error: %v", err)
	}

	want := "alpha\ndelta"
	if got != want {
		t.Fatalf("patched content mismatch\nwant: %q\ngot:  %q", want, got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("expected patched content to have no trailing newline")
	}
}

func TestParseUnifiedDiff_RejectsMultiFilePatch(t *testing.T) {
	diff := strings.TrimSpace(`
--- a/one.go
+++ b/one.go
@@ -1,1 +1,1 @@
-old
+new
--- a/two.go
+++ b/two.go
@@ -1,1 +1,1 @@
-old
+new
`)

	_, err := parseUnifiedDiff(diff)
	if err == nil {
		t.Fatal("expected parseUnifiedDiff to reject multi-file patch")
	}
	if !strings.Contains(err.Error(), "multi-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPatchExecutor_AppliesPatchAndRejectsEscapedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(root, "sub", "demo.txt")
	if err := os.WriteFile(filePath, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	rt := NewReadTracker()
	rt.MarkRead(root, "sub/demo.txt")

	exec := makePatchExecutor(root, NewMutationQueue(), rt)
	payload := PatchParams{
		Path: "sub/demo.txt",
		UnifiedDiff: strings.TrimSpace(`
@@ -1,3 +1,3 @@
 one
-two
+TWO
 three
`),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	result, err := exec(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("executor returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %s", result.Content)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(data) != "one\nTWO\nthree\n" {
		t.Fatalf("unexpected file content after patch: %q", string(data))
	}

	escapePayload := PatchParams{
		Path: "../escape.txt",
		UnifiedDiff: strings.TrimSpace(`
@@ -1,0 +1,1 @@
+nope
`),
	}
	raw, err = json.Marshal(escapePayload)
	if err != nil {
		t.Fatalf("marshal escape payload: %v", err)
	}

	result, err = exec(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("executor returned error for escaped path: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected escaped path to be rejected, got success: %s", result.Content)
	}
	if !strings.Contains(result.Content, "path escapes working directory") {
		t.Fatalf("unexpected escaped path error: %s", result.Content)
	}
}
