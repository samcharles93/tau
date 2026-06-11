package logger

import (
	"io"
	"log/slog"
)

// Format selects the slog handler encoding.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Options controls how a logger writes to the provided destination.
type Options struct {
	Level     slog.Leveler
	AddSource bool
	Format    Format
}

// New builds a slog.Logger that writes to any io.Writer.
// A nil writer falls back to io.Discard.
func New(writer io.Writer, opts Options) *slog.Logger {
	if writer == nil {
		writer = io.Discard
	}

	handlerOpts := &slog.HandlerOptions{
		AddSource: opts.AddSource,
	}
	if opts.Level != nil {
		handlerOpts.Level = opts.Level
	}

	switch opts.Format {
	case FormatJSON:
		return slog.New(slog.NewJSONHandler(writer, handlerOpts))
	default:
		return slog.New(slog.NewTextHandler(writer, handlerOpts))
	}
}

// NewText builds a text logger that writes to the provided destination.
func NewText(writer io.Writer, level slog.Leveler) *slog.Logger {
	return New(writer, Options{
		Level:  level,
		Format: FormatText,
	})
}

// NewJSON builds a JSON logger that writes to the provided destination.
func NewJSON(writer io.Writer, level slog.Leveler) *slog.Logger {
	return New(writer, Options{
		Level:  level,
		Format: FormatJSON,
	})
}

// SetDefault replaces slog's default logger and returns it.
func SetDefault(writer io.Writer, opts Options) *slog.Logger {
	logger := New(writer, opts)
	slog.SetDefault(logger)
	return logger
}
