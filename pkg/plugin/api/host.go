package api

import (
	"context"
	"errors"
)

// ErrPromptCanceled is returned by Host.Confirm/Input when the user cancels.
var ErrPromptCanceled = errors.New("prompt canceled by user")

// Host is the plugin-facing handle to tau host services. A plugin that wants to
// read config, query session state, or push notifications/logs back to the host
// implements HostAware; the host injects a Host once it calls Init.
type Host interface {
	// GetConfig returns a JSON-encoded value for the plugin's config block.
	// An empty key returns the whole block. found is false when unset.
	GetConfig(ctx context.Context, key string) (value string, found bool, err error)
	// SetConfig persists a JSON-encoded value under key for this plugin.
	SetConfig(ctx context.Context, key, value string) error
	// GetSessionState returns the JSON-encoded chat session state. An empty
	// sessionID targets the active session.
	GetSessionState(ctx context.Context, sessionID string) (stateJSON string, found bool, err error)
	// GetAvailableModels lists model ids the host knows about.
	GetAvailableModels(ctx context.Context) ([]string, error)
	// Notify pushes a user-visible notification to the host UI (TUI + web).
	Notify(ctx context.Context, level, message string) error
	// Confirm asks the user for confirmation. Returns false if canceled.
	Confirm(ctx context.Context, title, description string) (bool, error)
	// Input prompts the user for free-form text input.
	Input(ctx context.Context, title, placeholder string) (string, error)
	// Log forwards a structured log line to the host logger.
	Log(ctx context.Context, level, message string, fields map[string]string) error
}

// HostAware is implemented by Extensions that need to call back into the host.
// SetHost is invoked once, during Init, before any command or tool runs.
type HostAware interface {
	SetHost(h Host)
}

// hostClient adapts the generated HostServiceClient to the Host interface,
// binding the plugin's name onto config calls.
type hostClient struct {
	client     HostServiceClient
	pluginName string
}

var _ Host = (*hostClient)(nil)

func (h *hostClient) GetConfig(ctx context.Context, key string) (string, bool, error) {
	resp, err := h.client.GetConfig(ctx, &GetConfigRequest{PluginName: h.pluginName, Key: key})
	if err != nil {
		return "", false, err
	}
	return resp.Value, resp.Found, nil
}

func (h *hostClient) SetConfig(ctx context.Context, key, value string) error {
	_, err := h.client.SetConfig(ctx, &SetConfigRequest{PluginName: h.pluginName, Key: key, Value: value})
	return err
}

func (h *hostClient) GetSessionState(ctx context.Context, sessionID string) (string, bool, error) {
	resp, err := h.client.GetSessionState(ctx, &GetSessionStateRequest{SessionId: sessionID})
	if err != nil {
		return "", false, err
	}
	return resp.StateJson, resp.Found, nil
}

func (h *hostClient) GetAvailableModels(ctx context.Context) ([]string, error) {
	resp, err := h.client.GetAvailableModels(ctx, &GetAvailableModelsRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Models, nil
}

func (h *hostClient) Notify(ctx context.Context, level, message string) error {
	_, err := h.client.Notify(ctx, &NotifyRequest{Level: level, Message: message})
	return err
}

func (h *hostClient) Confirm(ctx context.Context, title, description string) (bool, error) {
	resp, err := h.client.Confirm(ctx, &ConfirmRequest{Title: title, Description: description})
	if err != nil {
		return false, err
	}
	if resp.Canceled {
		return false, ErrPromptCanceled
	}
	return resp.Confirmed, nil
}

func (h *hostClient) Input(ctx context.Context, title, placeholder string) (string, error) {
	resp, err := h.client.Input(ctx, &InputRequest{Title: title, Placeholder: placeholder})
	if err != nil {
		return "", err
	}
	if resp.Canceled {
		return "", ErrPromptCanceled
	}
	return resp.Value, nil
}

func (h *hostClient) Log(ctx context.Context, level, message string, fields map[string]string) error {
	_, err := h.client.Log(ctx, &LogRequest{Entry: &LogEntry{Level: level, Message: message, Fields: fields}})
	return err
}
