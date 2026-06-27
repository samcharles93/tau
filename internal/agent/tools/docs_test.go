package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestSearchDocsTool(t *testing.T) {
	tool := tools.NewSearchDocsTool()
	ctx := context.Background()

	t.Run("valid search", func(t *testing.T) {
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

	t.Run("empty query", func(t *testing.T) {
		params := json.RawMessage(`{"query": "  "}`)
		res, err := tool.Execute(ctx, params, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected error for empty query")
		}
	})
}

func TestReadDocTool(t *testing.T) {
	tool := tools.NewReadDocTool()
	ctx := context.Background()

	t.Run("valid read", func(t *testing.T) {
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

	t.Run("not found", func(t *testing.T) {
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
}
