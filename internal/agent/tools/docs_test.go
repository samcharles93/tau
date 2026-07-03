package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestDocsTool(t *testing.T) {
	tool := tools.NewDocsTool()
	ctx := context.Background()

	t.Run("search", func(t *testing.T) {
		params := json.RawMessage(`{"query": "provider"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error in tool output: %s", res.Content)
		}
		if strings.Contains(res.Content, "No matches found") {
			t.Errorf("expected search results to contain matches, but got: %q", res.Content)
		}
		if !strings.Contains(strings.ToLower(res.Content), "provider") {
			t.Errorf("expected search result to contain 'provider', got: %q", res.Content)
		}
	})

	t.Run("search with no matches", func(t *testing.T) {
		params := json.RawMessage(`{"query": "zzz-no-such-term-zzz"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error in tool output: %s", res.Content)
		}
		if !strings.Contains(res.Content, "No matches found") {
			t.Errorf("expected no matches, got: %q", res.Content)
		}
	})

	t.Run("read file", func(t *testing.T) {
		params := json.RawMessage(`{"path": "config-example.yaml"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error in tool output: %s", res.Content)
		}
		if !strings.Contains(res.Content, "Tau configuration example") {
			t.Errorf("expected content to contain 'Tau configuration example', got: %q", res.Content)
		}
	})

	t.Run("read not found", func(t *testing.T) {
		params := json.RawMessage(`{"path": "non-existent-file.md"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected error for non-existent file")
		}
		if !strings.Contains(res.Content, "not found") {
			t.Errorf("expected error message to say 'not found', got: %q", res.Content)
		}
	})

	t.Run("path traversal escape", func(t *testing.T) {
		params := json.RawMessage(`{"path": "../main.go"}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected error for path traversal attempt")
		}
		if !strings.Contains(res.Content, "escaping docs sandbox") {
			t.Errorf("expected sandbox error, got: %q", res.Content)
		}
	})

	t.Run("list with no parameters", func(t *testing.T) {
		params := json.RawMessage(`{}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error in tool output: %s", res.Content)
		}
		if !strings.Contains(res.Content, "config-example.yaml") {
			t.Errorf("expected listing to include 'config-example.yaml', got: %q", res.Content)
		}
	})
}
