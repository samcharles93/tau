package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteStore_SaveAndLoad(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("test-session", "claude-sonnet", "anthropic")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleSystem, Content: "You are helpful."},
		{Role: chat.ChatRoleUser, Content: "Hello"},
		{Role: chat.ChatRoleAssistant, Content: "Hi there!", ReasoningContent: "User said hello, I should greet them."},
	}
	state.LastUsage = chat.ChatUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	err := s.Save(ctx, state, 5*time.Second)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "test-session")
	require.NoError(t, err)

	assert.Equal(t, "test-session", loaded.SessionID)
	assert.Equal(t, "claude-sonnet", loaded.Model.ID)
	assert.Equal(t, "anthropic", loaded.Provider.Name)
	assert.Equal(t, "You are helpful.", loaded.SystemPrompt)
	assert.Equal(t, 3, len(loaded.Messages))

	assert.Equal(t, chat.ChatRoleSystem, loaded.Messages[0].Role)
	assert.Equal(t, "You are helpful.", loaded.Messages[0].Content)

	assert.Equal(t, chat.ChatRoleUser, loaded.Messages[1].Role)
	assert.Equal(t, "Hello", loaded.Messages[1].Content)

	assert.Equal(t, chat.ChatRoleAssistant, loaded.Messages[2].Role)
	assert.Equal(t, "Hi there!", loaded.Messages[2].Content)
	assert.Equal(t, "User said hello, I should greet them.", loaded.Messages[2].ReasoningContent)
}

func TestSQLiteStore_MessageTimestampsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// RFC3339 persistence is second-resolution, so use whole seconds.
	t0 := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	state := chatSessionFixture("ts-session", "claude-sonnet", "anthropic")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "Hello", CreatedAt: t0},
		{Role: chat.ChatRoleAssistant, Content: "Hi!", CreatedAt: t0.Add(7 * time.Second)},
	}

	require.NoError(t, s.Save(ctx, state, 5*time.Second))

	loaded, err := s.Load(ctx, "ts-session")
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded.Messages))

	// Explicit per-message timestamps survive the round-trip exactly,
	// rather than being synthesised from the session start.
	assert.True(t, loaded.Messages[0].CreatedAt.Equal(t0),
		"user CreatedAt = %s, want %s", loaded.Messages[0].CreatedAt, t0)
	assert.True(t, loaded.Messages[1].CreatedAt.Equal(t0.Add(7*time.Second)),
		"assistant CreatedAt = %s, want %s", loaded.Messages[1].CreatedAt, t0.Add(7*time.Second))
}

func TestSQLiteStore_MessageTimestampFallback(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Messages without a CreatedAt (e.g. persisted before per-message
	// timestamps were tracked) fall back to a synthesised, ascending time
	// derived from the session start.
	state := chatSessionFixture("legacy-session", "gpt-4", "openai")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "first"},
		{Role: chat.ChatRoleAssistant, Content: "second"},
	}

	require.NoError(t, s.Save(ctx, state, 0))

	loaded, err := s.Load(ctx, "legacy-session")
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded.Messages))

	assert.False(t, loaded.Messages[0].CreatedAt.IsZero(), "fallback timestamp should be set")
	assert.True(t, loaded.Messages[1].CreatedAt.After(loaded.Messages[0].CreatedAt),
		"synthesised timestamps should be strictly ascending")
}

func TestSQLiteStore_SaveWithToolCalls(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("tool-session", "gpt-4", "openai")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "Read /tmp/x"},
		{
			Role:    chat.ChatRoleAssistant,
			Content: "",
			ToolCalls: []chat.ChatToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: chat.ChatFunctionCall{
						Name:      "read",
						Arguments: `{"path":"/tmp/x"}`,
					},
				},
			},
		},
		{
			Role:       chat.ChatRoleTool,
			Content:    "file contents here",
			ToolCallID: "call_1",
		},
		{Role: chat.ChatRoleAssistant, Content: "The file contains: file contents here"},
	}

	err := s.Save(ctx, state, 2*time.Second)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "tool-session")
	require.NoError(t, err)

	require.Equal(t, 4, len(loaded.Messages))

	// Verify tool call round-trip.
	assistant := loaded.Messages[1]
	require.Equal(t, 1, len(assistant.ToolCalls))
	assert.Equal(t, "call_1", assistant.ToolCalls[0].ID)
	assert.Equal(t, "read", assistant.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"path":"/tmp/x"}`, assistant.ToolCalls[0].Function.Arguments)

	// Verify tool result round-trip.
	tool := loaded.Messages[2]
	assert.Equal(t, "call_1", tool.ToolCallID)
	assert.Equal(t, "file contents here", tool.Content)
}

func TestSQLiteStore_SaveUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("update-session", "model-a", "prov-a")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "First message"},
	}
	err := s.Save(ctx, state, 1*time.Second)
	require.NoError(t, err)

	// Add more messages and save again.
	state.Messages = append(
		state.Messages,
		chat.ChatMessage{Role: chat.ChatRoleAssistant, Content: "First reply"},
		chat.ChatMessage{Role: chat.ChatRoleUser, Content: "Second message"},
	)
	state.LastUsage = chat.ChatUsage{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
	}
	err = s.Save(ctx, state, 3*time.Second)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "update-session")
	require.NoError(t, err)
	assert.Equal(t, 3, len(loaded.Messages))
	assert.Equal(t, "Second message", loaded.Messages[2].Content)
}

func TestSQLiteStore_ListAndPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create sessions with staggered timestamps.
	now := time.Now().UTC()
	for i := range 5 {
		state := chatSessionFixture("sess-"+string(rune('a'+i)), "model", "prov")
		state.CreatedAt = now.Add(-time.Duration(4-i) * time.Hour)
		state.UpdatedAt = state.CreatedAt
		state.Messages = []chat.ChatMessage{
			{Role: chat.ChatRoleUser, Content: "msg"},
		}
		err := s.Save(ctx, state, 0)
		require.NoError(t, err)
	}

	// Page 1: first 3, newest first.
	page1, cursor, err := s.List(ctx, 3, "")
	require.NoError(t, err)
	require.Equal(t, 3, len(page1))
	assert.Equal(t, "sess-e", page1[0].ID) // newest
	assert.Equal(t, "sess-d", page1[1].ID)
	assert.Equal(t, "sess-c", page1[2].ID)
	assert.NotEmpty(t, cursor)

	// Page 2: remaining 2.
	page2, cursor2, err := s.List(ctx, 3, cursor)
	require.NoError(t, err)
	require.Equal(t, 2, len(page2))
	assert.Equal(t, "sess-b", page2[0].ID)
	assert.Equal(t, "sess-a", page2[1].ID)
	assert.Empty(t, cursor2) // no more pages
}

func TestSQLiteStore_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("delete-me", "model", "prov")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "bye"},
	}
	err := s.Save(ctx, state, 0)
	require.NoError(t, err)

	// Verify it exists.
	count, err := s.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = s.Delete(ctx, "delete-me")
	require.NoError(t, err)

	// Verify it's gone.
	count, err = s.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	_, err = s.Load(ctx, "delete-me")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSQLiteStore_DeleteNonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.Delete(ctx, "no-such-session")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSQLiteStore_LoadNonExistent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Load(ctx, "no-such-session")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSQLiteStore_ExportMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("export-me", "model", "prov")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleSystem, Content: "System prompt"},
		{Role: chat.ChatRoleUser, Content: "Hello"},
		{Role: chat.ChatRoleAssistant, Content: "Hi", ReasoningContent: "thought process"},
	}
	err := s.Save(ctx, state, 0)
	require.NoError(t, err)

	ch, errCh := s.ExportMessages(ctx, "export-me")

	var lines []string
	for line := range ch {
		lines = append(lines, string(line))
	}

	// Check for errors.
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}

	require.Equal(t, 3, len(lines))
	assert.Contains(t, lines[0], `"role":"system"`)
	assert.Contains(t, lines[0], `"System prompt"`)
	assert.Contains(t, lines[1], `"role":"user"`)
	assert.Contains(t, lines[1], `"Hello"`)
	assert.Contains(t, lines[2], `"reasoning_content":"thought process"`)
}

func TestSQLiteStore_ExportJSONLFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("file-export", "model", "prov")
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "test"},
	}
	err := s.Save(ctx, state, 0)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "export.jsonl")

	err = store.ExportSessionAsJSONL(ctx, s, "file-export", outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"role":"user"`)
	assert.Contains(t, string(data), `"test"`)

	// Verify no temp file left behind.
	_, err = os.Stat(outputPath + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file should be cleaned up")
}

func TestSQLiteStore_EmptyMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("empty-session", "model", "prov")
	// No messages.
	err := s.Save(ctx, state, 0)
	require.NoError(t, err)

	loaded, err := s.Load(ctx, "empty-session")
	require.NoError(t, err)
	assert.Equal(t, 0, len(loaded.Messages))
}

func TestSQLiteStore_MigrationIdempotent(t *testing.T) {
	// Open the store twice — migration should be idempotent.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s1, err := store.NewSQLiteStore(context.Background(), dbPath, tmpDir)
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	s2, err := store.NewSQLiteStore(context.Background(), dbPath, tmpDir)
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}

func TestSQLiteStore_CostCalculation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("cost-session", "expensive-model", "openai")
	state.Model.Config.Cost = config.CostConfig{
		Input:  3.0,  // $3 per 1M input tokens
		Output: 15.0, // $15 per 1M output tokens
	}
	state.Messages = []chat.ChatMessage{
		{Role: chat.ChatRoleUser, Content: "expensive"},
	}
	state.LastUsage = chat.ChatUsage{
		PromptTokens:     500_000, // 0.5M
		CompletionTokens: 200_000, // 0.2M
		TotalTokens:      700_000,
	}

	err := s.Save(ctx, state, 1*time.Second)
	require.NoError(t, err)

	summaries, _, err := s.List(ctx, 1, "")
	require.NoError(t, err)
	require.Equal(t, 1, len(summaries))

	// Expected cost: (500K * 3.0 / 1M) + (200K * 15.0 / 1M) = 1.5 + 3.0 = 4.5
	assert.InDelta(t, 4.5, summaries[0].Cost, 0.001)
}

// Helpers

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := store.NewSQLiteStore(context.Background(), dbPath, tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func chatSessionFixture(id, modelID, provider string) chat.ChatSessionState {
	now := time.Now().UTC()
	return chat.ChatSessionState{
		SessionID:    id,
		Model:        chat.ChatModelRef{ID: modelID},
		Provider:     config.ProviderConfig{Name: provider},
		SystemPrompt: "You are helpful.",
		Status:       chat.ChatSessionIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}
