package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEdits(t *testing.T) {
	cases := []struct {
		name    string
		content string
		edits   []EditAction
		want    string
		wantErr string
	}{
		{
			name:    "single edit",
			content: "func a() {}\nfunc b() {}\n",
			edits:   []EditAction{{OldText: "func a()", NewText: "func alpha()"}},
			want:    "func alpha() {}\nfunc b() {}\n",
		},
		{
			name:    "multiple disjoint edits applied against original",
			content: "one\ntwo\nthree\n",
			edits: []EditAction{
				{OldText: "three", NewText: "3"},
				{OldText: "one", NewText: "1"},
			},
			want: "1\ntwo\n3\n",
		},
		{
			name:    "replace all",
			content: "x = a; y = a; z = a\n",
			edits:   []EditAction{{OldText: "a", NewText: "b", ReplaceAll: true}},
			want:    "x = b; y = b; z = b\n",
		},
		{
			name:    "not found",
			content: "hello\n",
			edits:   []EditAction{{OldText: "goodbye", NewText: "farewell"}},
			wantErr: "old_text not found",
		},
		{
			name:    "ambiguous without replace_all",
			content: "dup\ndup\ndup\n",
			edits:   []EditAction{{OldText: "dup", NewText: "uniq"}},
			wantErr: "appears 3 times",
		},
		{
			name:    "empty old_text",
			content: "hello\n",
			edits:   []EditAction{{OldText: "", NewText: "x"}},
			wantErr: "must not be empty",
		},
		{
			name:    "identical old and new",
			content: "hello\n",
			edits:   []EditAction{{OldText: "hello", NewText: "hello"}},
			wantErr: "identical",
		},
		{
			name:    "overlapping edits rejected",
			content: "abcdef\n",
			edits: []EditAction{
				{OldText: "abcd", NewText: "x"},
				{OldText: "cdef", NewText: "y"},
			},
			wantErr: "overlap",
		},
		{
			name:    "fuzzy match forgives trailing whitespace in file",
			content: "line one  \nline two\n", // trailing spaces on line one
			edits:   []EditAction{{OldText: "line one\nline two", NewText: "replaced"}},
			want:    "replaced\n",
		},
		{
			name:    "fuzzy match forgives trailing whitespace in old_text",
			content: "line one\nline two\n",
			edits:   []EditAction{{OldText: "line one   \nline two", NewText: "replaced"}},
			want:    "replaced\n",
		},
		{
			name:    "atomic: second edit failing means no content returned",
			content: "one\ntwo\n",
			edits: []EditAction{
				{OldText: "one", NewText: "1"},
				{OldText: "missing", NewText: "x"},
			},
			wantErr: "edit 2: old_text not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyEdits(tc.content, tc.edits)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got result:\n%s", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("result mismatch:\ngot:  %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestEditTool_PreservesCRLFAndBOM(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(path, []byte("\ufefffoo\r\nbar\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewEditTool(tmp, NewMutationQueue(), nil)
	// old_text uses LF: matching must succeed against the CRLF file.
	params := `{"path": "f.txt", "edits": [{"old_text": "foo\nbar", "new_text": "baz\nqux"}]}`
	res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", res.Content)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "\ufeffbaz\r\nqux\r\n" {
		t.Fatalf("BOM or CRLF not preserved, got: %q", string(got))
	}
}

func TestEditTool_AtomicOnDisk(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	original := "one\ntwo\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewEditTool(tmp, NewMutationQueue(), nil)
	// First edit is valid, second fails: the file must be left untouched.
	params := `{"path": "f.txt", "edits": [{"old_text": "one", "new_text": "1"}, {"old_text": "missing", "new_text": "x"}]}`
	res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got: %s", res.Content)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("file was partially modified on failure: %q", string(got))
	}
}

func TestApplyEdits_ExactMatchPreservesTrailingWhitespace(t *testing.T) {
	// A single-line old_text is an exact substring match even when the file
	// line has trailing whitespace, which is preserved around the edit.
	content := "keep\ntarget  \nkeep\n"
	got, err := applyEdits(content, []EditAction{{OldText: "target", NewText: "T"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "keep\nT  \nkeep\n" {
		t.Fatalf("result mismatch: %q", got)
	}
}
