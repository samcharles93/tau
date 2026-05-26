package tools

import (
	"fmt"
	"strings"
)

const (
	// DefaultMaxBytes is the maximum byte size for tool output sent to the LLM.
	DefaultMaxBytes = 50 * 1024 // 50KB

	// DefaultMaxLines is the maximum line count for tool output sent to the LLM.
	DefaultMaxLines = 2000
)

// TruncationResult holds the potentially truncated content and metadata.
type TruncationResult struct {
	Content      string
	Truncated    bool
	OriginalSize int
	OriginalLine int
}

// TruncateHead keeps the first N lines/bytes, dropping the tail.
// Good for file reads, search results.
func TruncateHead(content string, maxLines, maxBytes int) TruncationResult {
	originalSize := len(content)
	originalLines := 0
	if content != "" {
		originalLines = strings.Count(content, "\n") + 1
	}

	result := TruncationResult{
		Content:      content,
		OriginalSize: originalSize,
		OriginalLine: originalLines,
	}

	if originalSize <= maxBytes && originalLines <= maxLines {
		return result
	}

	lines := strings.SplitAfter(content, "\n")
	var b strings.Builder
	lineCount := 0

	for _, line := range lines {
		if lineCount >= maxLines || b.Len()+len(line) > maxBytes {
			result.Truncated = true
			break
		}
		b.WriteString(line)
		lineCount++
	}

	if result.Truncated {
		kept := b.String()
		fmt.Fprintf(
			&b,
			"\n\n[truncated: showing %d/%d lines, %s/%s]",
			lineCount, originalLines,
			FormatSize(len(kept)), FormatSize(originalSize),
		)
	}

	result.Content = b.String()
	return result
}

// TruncateTail keeps the last N lines/bytes, dropping the head.
// Good for logs, command output.
func TruncateTail(content string, maxLines, maxBytes int) TruncationResult {
	originalSize := len(content)
	originalLines := 0
	if content != "" {
		originalLines = strings.Count(content, "\n") + 1
	}

	result := TruncationResult{
		Content:      content,
		OriginalSize: originalSize,
		OriginalLine: originalLines,
	}

	if originalSize <= maxBytes && originalLines <= maxLines {
		return result
	}

	lines := strings.Split(content, "\n")

	// Work backwards to find start index.
	startIdx := len(lines)
	byteCount := 0
	lineCount := 0

	for i := len(lines) - 1; i >= 0; i-- {
		lineLen := len(lines[i])
		if i < len(lines)-1 {
			lineLen++ // account for the \n
		}
		if lineCount >= maxLines || byteCount+lineLen > maxBytes {
			break
		}
		byteCount += lineLen
		lineCount++
		startIdx = i
	}

	if startIdx > 0 {
		result.Truncated = true
		kept := strings.Join(lines[startIdx:], "\n")
		result.Content = fmt.Sprintf(
			"[truncated: showing last %d/%d lines, %s/%s]\n\n%s",
			lineCount, originalLines,
			FormatSize(len(kept)), FormatSize(originalSize),
			kept,
		)
	}

	return result
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
