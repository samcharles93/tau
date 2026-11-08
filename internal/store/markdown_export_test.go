package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
)

// markdownFixture builds a session exercising every rendered message shape:
// system, user with attachment, assistant with reasoning + tool call, and a
// tool result with metadata.
func markdownFixture(id string) chat.ChatSessionState {
	state := chatSessionFixture(id, "claude-sonnet-4", "anthropic")
	state.LastUsage = chat.ChatUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleSystem, Content: "You are helpful."},
		{
			Role:    chat.ChatRoleUser,
			Content: "fix: what is in [Image #1]?",
			Attachments: []chat.ChatAttachment{
				{Placeholder: "[Image #1]", MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}},
			},
		},
		{
			Role:             chat.ChatRoleAssistant,
			Content:          "Let me look.",
			ReasoningContent: "The user attached an image; I should read it.",
			ToolCalls: []chat.ChatToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: chat.ChatFunctionCall{
					Name:      "read",
					Arguments: `{"path":"main.go"}`,
				},
			}},
		},
		{
			Role:       chat.ChatRoleTool,
			ToolCallID: "call_1",
			Content:    "package main\n\n```go\nfunc main() {}\n```",
			ToolResult: &chat.ToolResultMetadata{
				ToolName:    "read",
				Status:      "ok",
				Duration:    12 * time.Millisecond,
				ResultBytes: 13,
			},
		},
		{Role: chat.ChatRoleAssistant, Content: "It is a hello-world program."},
	}
	return state
}

func TestExportSessionAsMarkdown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, markdownFixture("md-session"), 5*time.Second))

	doc, err := store.ExportSessionAsMarkdown(ctx, s, "md-session")
	require.NoError(t, err)

	// Frontmatter: YAML block with session metadata, strings quoted so
	// colons in titles stay valid YAML.
	require.True(t, strings.HasPrefix(doc, "---\n"), "doc must open with frontmatter:\n%s", doc)
	assert.Contains(t, doc, `session_id: "md-session"`)
	assert.Contains(t, doc, `model: "claude-sonnet-4"`)
	assert.Contains(t, doc, `provider: "anthropic"`)
	assert.Contains(t, doc, "total_tokens: 150")
	assert.Contains(t, doc, "message_count: 5")
	assert.Contains(t, doc, `format: "markdown"`)
	// Title derived from the first user message, quoted because of the colon.
	assert.Contains(t, doc, `title: "fix: what is in [Image #1]?"`)
	frontmatterEnd := strings.Index(doc[4:], "\n---\n")
	require.NotEqual(t, -1, frontmatterEnd, "frontmatter must close:\n%s", doc)

	// Role-based headers.
	assert.Contains(t, doc, "## System")
	assert.Contains(t, doc, "## User")
	assert.Contains(t, doc, "## Assistant")
	assert.Contains(t, doc, "## Tool `read`")

	// Document heading uses the session title.
	assert.Contains(t, doc, "# fix: what is in [Image #1]?")

	// Reasoning in a <details> block.
	assert.Contains(t, doc, "<details>\n<summary>Reasoning</summary>")
	assert.Contains(t, doc, "The user attached an image; I should read it.")

	// Tool call as a fenced JSON block with pretty-printed arguments.
	assert.Contains(t, doc, "**Tool call:**")
	assert.Contains(t, doc, "```json\n{")
	assert.Contains(t, doc, `"name": "read"`)
	assert.Contains(t, doc, `"path": "main.go"`)

	// Tool result content is fenced, and its fence is extended because the
	// content itself contains a ``` run.
	assert.Contains(t, doc, "````\npackage main")
	assert.Contains(t, doc, "````\n")

	// Tool result metadata line.
	assert.Contains(t, doc, "_status: ok · duration: 12ms · 13 bytes_")

	// Attachment listing.
	assert.Contains(t, doc, "- attachment [Image #1] (image/png, 4 bytes)")
}

func TestExportSessionAsMarkdown_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := store.ExportSessionAsMarkdown(context.Background(), s, "nope")
	require.Error(t, err)
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "want ErrSessionNotFound, got %v", err)
}

func TestExportSessionAsMarkdownFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, markdownFixture("file-session"), time.Second))

	out := filepath.Join(t.TempDir(), "session.md")
	require.NoError(t, store.ExportSessionAsMarkdownFile(ctx, s, "file-session", out))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), `session_id: "file-session"`)
	assert.Contains(t, string(data), "## Assistant")
	// Atomic write leaves no temp file behind.
	_, statErr := os.Stat(out + ".tmp")
	assert.True(t, os.IsNotExist(statErr))

	// Exporting to an unwritable path fails and cleans up its temp file.
	bad := filepath.Join(t.TempDir(), "missing-dir", "session.md")
	err = store.ExportSessionAsMarkdownFile(ctx, s, "file-session", bad)
	require.Error(t, err)
	_, statErr = os.Stat(bad + ".tmp")
	assert.True(t, os.IsNotExist(statErr))

	// Missing session surfaces the not-found error, not a write error.
	err = store.ExportSessionAsMarkdownFile(ctx, s, "nope", out)
	assert.True(t, errors.Is(err, store.ErrSessionNotFound))
}

// badLineStore emits one undecodable line followed by more lines than the
// channel buffer, so a consumer that fails to drain on decode error deadlocks.
type badLineStore struct {
	store.SessionStore
	summary store.SessionSummary
	lines   int
}

func (s *badLineStore) Summary(context.Context, string) (store.SessionSummary, error) {
	return s.summary, nil
}

func (s *badLineStore) ExportMessages(ctx context.Context, _ string) (<-chan []byte, <-chan error) {
	out := make(chan []byte, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		out <- []byte("{not json}\n")
		for i := 0; i < s.lines; i++ {
			select {
			case out <- []byte(`{"role":"user","content":"x"}` + "\n"):
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, errs
}

func TestExportSessionAsMarkdown_DecodeErrorDrainsProducer(t *testing.T) {
	fake := &badLineStore{
		summary: store.SessionSummary{ID: "fake", ModelID: "m", Provider: "p"},
		lines:   200, // more than the 64-slot buffer
	}
	_, err := store.ExportSessionAsMarkdown(context.Background(), fake, "fake")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode message")
}

func TestSQLiteStore_Summary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := markdownFixture("sum-session")
	state.LastUsage = chat.ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	require.NoError(t, s.Save(ctx, state, 2500*time.Millisecond))

	sum, err := s.Summary(ctx, "sum-session")
	require.NoError(t, err)
	assert.Equal(t, "sum-session", sum.ID)
	assert.Equal(t, "claude-sonnet-4", sum.ModelID)
	assert.Equal(t, "anthropic", sum.Provider)
	assert.Equal(t, 5, sum.MessageCount)
	assert.Equal(t, 10, sum.InputTokens)
	assert.Equal(t, 5, sum.OutputTokens)
	assert.Equal(t, 15, sum.TotalTokens)
	assert.Equal(t, int64(2500), sum.DurationMs)
	assert.Equal(t, "fix: what is in [Image #1]?", sum.Title)

	_, err = s.Summary(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrSessionNotFound), "want ErrSessionNotFound, got %v", err)
}

func TestNormalizeExportFormat(t *testing.T) {
	assert.Equal(t, chat.ExportFormatJSONL, chat.NormalizeExportFormat(""))
	assert.Equal(t, chat.ExportFormatJSONL, chat.NormalizeExportFormat("jsonl"))
	assert.Equal(t, chat.ExportFormatJSONL, chat.NormalizeExportFormat("JSON"))
	assert.Equal(t, chat.ExportFormatJSONL, chat.NormalizeExportFormat("bogus"))
	assert.Equal(t, chat.ExportFormatMarkdown, chat.NormalizeExportFormat("md"))
	assert.Equal(t, chat.ExportFormatMarkdown, chat.NormalizeExportFormat(" Markdown "))

	assert.True(t, chat.IsExportFormat("md"))
	assert.True(t, chat.IsExportFormat("jsonl"))
	assert.False(t, chat.IsExportFormat("/tmp/out.jsonl"))
	assert.False(t, chat.IsExportFormat(""))
}
