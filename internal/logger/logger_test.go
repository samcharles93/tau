package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTextWritesToProvidedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewText(&buf, slog.LevelDebug)
	logger.Debug("hello", "target", "buffer")

	output := buf.String()
	if !strings.Contains(output, "level=DEBUG") {
		t.Fatalf("expected debug level in output, got %q", output)
	}
	if !strings.Contains(output, "msg=hello") {
		t.Fatalf("expected message in output, got %q", output)
	}
	if !strings.Contains(output, "target=buffer") {
		t.Fatalf("expected attributes in output, got %q", output)
	}
}

func TestNewJSONWritesStructuredOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewJSON(&buf, slog.LevelInfo)
	logger.Info("hello", "target", "buffer")

	output := buf.String()
	if !strings.Contains(output, "\"level\":\"INFO\"") {
		t.Fatalf("expected json level in output, got %q", output)
	}
	if !strings.Contains(output, "\"msg\":\"hello\"") {
		t.Fatalf("expected json message in output, got %q", output)
	}
	if !strings.Contains(output, "\"target\":\"buffer\"") {
		t.Fatalf("expected json attributes in output, got %q", output)
	}
}

func TestNilWriterFallsBackToDiscard(t *testing.T) {
	t.Parallel()

	logger := New(nil, Options{Level: slog.LevelInfo})
	logger.Info("hello")
}
