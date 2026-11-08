package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
)

// ExportSessionAsMarkdown renders a saved session as a single Markdown
// document. The document opens with a YAML frontmatter block carrying the
// session metadata (model, provider, token usage, cost, duration), followed
// by one role-headed section per message: user/assistant/system content is
// passed through as Markdown, reasoning renders inside a <details> block,
// assistant tool calls render as fenced JSON blocks, and tool results render
// as fenced code blocks with their execution metadata.
func ExportSessionAsMarkdown(ctx context.Context, st SessionStore, id string) (string, error) {
	summary, err := st.Summary(ctx, id)
	if err != nil {
		return "", err
	}

	msgs, err := collectExportMessages(ctx, st, id)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	writeMarkdownFrontmatter(&b, summary)
	b.WriteString("\n# ")
	b.WriteString(markdownHeading(summary.Title, summary.ID))
	b.WriteString("\n")

	for i := range msgs {
		writeMarkdownMessage(&b, &msgs[i])
	}
	return b.String(), nil
}

// ExportSessionAsMarkdownFile renders a session as Markdown and writes it to
// outputPath atomically (temp file + rename), matching ExportSessionAsJSONL.
func ExportSessionAsMarkdownFile(ctx context.Context, st SessionStore, id, outputPath string) error {
	doc, err := ExportSessionAsMarkdown(ctx, st, id)
	if err != nil {
		return err
	}
	return writeFileAtomic(outputPath, []byte(doc), 0o644)
}

// collectExportMessages drains the ExportMessages stream into decoded
// messages. A decode error cancels the producer and drains the channel so
// the producer goroutine can exit (same contract as streamMessagesToJSONL).
func collectExportMessages(ctx context.Context, st SessionStore, id string) ([]chat.ChatMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch, errCh := st.ExportMessages(ctx, id)

	var msgs []chat.ChatMessage
	var decodeErr error
	for line := range ch {
		var m chat.ChatMessage
		if err := json.Unmarshal(line, &m); err != nil {
			decodeErr = fmt.Errorf("store: decode message: %w", err)
			cancel()
			break
		}
		msgs = append(msgs, m)
	}
	// Drain remaining messages so the export goroutine can exit cleanly.
	for range ch {
	}

	if decodeErr != nil {
		return nil, decodeErr
	}
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("store: export messages: %w", err)
		}
	default:
	}
	return msgs, nil
}

// writeMarkdownFrontmatter emits the YAML frontmatter block with session
// metadata. Strings are always double-quoted via strconv.Quote, which is
// valid YAML and safely escapes colons, quotes, and control characters.
func writeMarkdownFrontmatter(b *strings.Builder, s SessionSummary) {
	b.WriteString("---\n")
	yamlField(b, "title", s.Title)
	yamlField(b, "session_id", s.ID)
	yamlField(b, "provider", s.Provider)
	yamlField(b, "model", s.ModelID)
	yamlField(b, "status", s.Status)
	yamlField(b, "created_at", s.CreatedAt.UTC().Format(time.RFC3339))
	yamlField(b, "updated_at", s.UpdatedAt.UTC().Format(time.RFC3339))
	yamlInt(b, "message_count", int64(s.MessageCount))
	yamlInt(b, "input_tokens", int64(s.InputTokens))
	yamlInt(b, "output_tokens", int64(s.OutputTokens))
	yamlInt(b, "total_tokens", int64(s.TotalTokens))
	fmt.Fprintf(b, "cost: %.6f\n", s.Cost)
	yamlInt(b, "duration_ms", s.DurationMs)
	yamlInt(b, "tool_calls", int64(s.ToolCalls))
	yamlInt(b, "tool_errors", int64(s.ToolErrors))
	yamlField(b, "exported_at", time.Now().UTC().Format(time.RFC3339))
	yamlField(b, "format", "markdown")
	b.WriteString("---\n")
}

func yamlField(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "%s: %s\n", key, strconv.Quote(value))
}

func yamlInt(b *strings.Builder, key string, value int64) {
	fmt.Fprintf(b, "%s: %d\n", key, value)
}

// markdownHeading picks the document title: the session title when present,
// otherwise the session ID.
func markdownHeading(title, id string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return "Session " + id
}

// writeMarkdownMessage appends one message section. Sections are separated
// by a blank line; the header is a role-based ## heading.
func writeMarkdownMessage(b *strings.Builder, m *chat.ChatMessage) {
	b.WriteString("\n## ")
	switch m.Role {
	case chat.ChatRoleUser:
		b.WriteString("User")
	case chat.ChatRoleAssistant:
		b.WriteString("Assistant")
	case chat.ChatRoleTool:
		b.WriteString("Tool")
		if m.ToolResult != nil && m.ToolResult.ToolName != "" {
			b.WriteString(" `")
			b.WriteString(m.ToolResult.ToolName)
			b.WriteString("`")
		}
	default:
		b.WriteString("System")
	}
	b.WriteString("\n\n")

	if m.ReasoningContent != "" {
		b.WriteString("<details>\n<summary>Reasoning</summary>\n\n")
		b.WriteString(strings.TrimRight(m.ReasoningContent, "\n"))
		b.WriteString("\n\n</details>\n\n")
	}

	if m.Content != "" {
		if m.Role == chat.ChatRoleTool {
			// Tool output is not Markdown - render it verbatim in a fence.
			writeFencedBlock(b, "", m.Content)
		} else {
			b.WriteString(strings.TrimRight(m.Content, "\n"))
			b.WriteString("\n")
		}
	}

	for i := range m.ToolCalls {
		writeToolCallBlock(b, &m.ToolCalls[i])
	}

	if m.Role == chat.ChatRoleTool && m.ToolResult != nil {
		writeToolResultMeta(b, m.ToolResult)
	}

	for i := range m.Attachments {
		a := &m.Attachments[i]
		fmt.Fprintf(b, "\n- attachment %s (%s, %d bytes)\n", a.Placeholder, a.MediaType, len(a.Data))
	}
}

// writeToolCallBlock renders one assistant tool call as a fenced, indented
// JSON block. Arguments embed as a structured JSON value when they parse,
// falling back to the raw string otherwise.
func writeToolCallBlock(b *strings.Builder, tc *chat.ChatToolCall) {
	argsVal := any(tc.Function.Arguments)
	if v, err := decodeJSON(tc.Function.Arguments); err == nil {
		argsVal = v
	}
	call := map[string]any{
		"id":   tc.ID,
		"type": tc.Type,
		"function": map[string]any{
			"name":      tc.Function.Name,
			"arguments": argsVal,
		},
	}
	raw, err := json.MarshalIndent(call, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"name":%q,"arguments":%q}`, tc.Function.Name, tc.Function.Arguments))
	}
	b.WriteString("\n**Tool call:**\n\n")
	writeFencedBlock(b, "json", string(raw))
}

// writeToolResultMeta renders the authoritative execution metadata of a tool
// result as an italic line after the result block.
func writeToolResultMeta(b *strings.Builder, meta *chat.ToolResultMetadata) {
	parts := []string{"status: " + meta.Status}
	if meta.Duration > 0 {
		parts = append(parts, "duration: "+meta.Duration.String())
	}
	if meta.ResultBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", meta.ResultBytes))
	}
	if meta.Truncated {
		parts = append(parts, "truncated")
	}
	if meta.ErrorKind != "" {
		parts = append(parts, "error: "+meta.ErrorKind)
	}
	fmt.Fprintf(b, "\n_%s_\n", strings.Join(parts, " · "))
}

// writeFencedBlock writes content in a fenced code block, extending the
// fence when the content itself contains a run of backticks.
func writeFencedBlock(b *strings.Builder, lang, content string) {
	fence := "```"
	for strings.Contains(content, fence) {
		fence += "`"
	}
	b.WriteString(fence)
	b.WriteString(lang)
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(content, "\n"))
	b.WriteString("\n")
	b.WriteString(fence)
	b.WriteString("\n")
}

// decodeJSON parses s as JSON into a generic value; the error is non-nil
// when s is not valid JSON.
func decodeJSON(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// writeFileAtomic writes data to path via a temp file + rename so a crash
// mid-write never leaves a partial export at path. It mirrors the unexported
// helper of the same name in internal/agent/tools, which cannot be shared
// here: tools imports store, so the dependency points the other way.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("store: create temp export: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("store: write export: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: close export: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename temp export: %w", err)
	}
	return nil
}
