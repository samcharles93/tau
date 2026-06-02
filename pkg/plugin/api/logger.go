package api

import (
	"context"
	"fmt"
)

// PluginLogger is a helper for plugins to emit logs back to the host via gRPC.
// Plugins can call this to send structured logs to tau's main logger.
type PluginLogger struct {
	client ExtensionServiceClient
}

// NewPluginLogger creates a logger for use within a plugin.
// It requires the gRPC client to communicate back to the host.
func NewPluginLogger(client ExtensionServiceClient) *PluginLogger {
	return &PluginLogger{client: client}
}

// Debug logs at debug level.
func (pl *PluginLogger) Debug(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "debug", msg, fields)
}

// Info logs at info level.
func (pl *PluginLogger) Info(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "info", msg, fields)
}

// Warn logs at warn level.
func (pl *PluginLogger) Warn(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "warn", msg, fields)
}

// Error logs at error level.
func (pl *PluginLogger) Error(ctx context.Context, msg string, fields map[string]string) error {
	return pl.log(ctx, "error", msg, fields)
}

// WithError is a helper to add an error to the fields map.
func WithError(fields map[string]string, err error) map[string]string {
	if fields == nil {
		fields = make(map[string]string)
	}
	if err != nil {
		fields["error"] = err.Error()
	}
	return fields
}

func (pl *PluginLogger) log(ctx context.Context, level, msg string, fields map[string]string) error {
	if pl.client == nil {
		return fmt.Errorf("plugin logger: no gRPC client available")
	}
	_, err := pl.client.Log(ctx, &LogRequest{
		Entry: &LogEntry{
			Level:   level,
			Message: msg,
			Fields:  fields,
		},
	})
	return err
}
