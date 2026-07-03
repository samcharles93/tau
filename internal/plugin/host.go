package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samcharles93/tau/pkg/plugin/api"
)

// Notifier pushes a user-visible notification to the host UI (TUI + web).
type Notifier func(level, message string)

// InteractiveHandler lets plugins prompt the user for confirmation or text input.
// It is set by the coordinator via SetInteractiveHandler.
type InteractiveHandler interface {
	Confirm(ctx context.Context, title, description string) (bool, error)
	Input(ctx context.Context, title, placeholder string) (string, error)
}

// ViewRenderer lets plugins open, update, and close persistent panels in the
// host UI via the HostService RenderView/CloseView RPCs. It is set by the
// coordinator via Manager.SetViewRenderer.
type ViewRenderer interface {
	RenderView(ctx context.Context, pluginName string, view *api.View) error
	CloseView(ctx context.Context, pluginName, viewID string) error
}

// hostService implements api.HostServiceServer. A single instance is shared by
// all plugins; requests carry the plugin name so config is correctly scoped.
type hostService struct {
	api.UnimplementedHostServiceServer

	logger *slog.Logger

	// config holds the static `plugins.<name>` blocks from config.yaml.
	config map[string]map[string]any

	// kv persists SetConfig values across runs.
	kv *kvStore

	// Optional host capabilities. Nil fields degrade gracefully.
	notify            Notifier
	models            func() []string
	sessionState      func(sessionID string) (stateJSON string, found bool)
	interactivePrompt InteractiveHandler
	viewRenderer      ViewRenderer

	// views tracks per-plugin view ownership (pluginName -> set of raw view
	// ids) so CloseView can reject requests for views a plugin doesn't own,
	// and Unload/reload can close every view a plugin left open.
	viewsMu           sync.Mutex
	views             map[string]map[string]struct{}
	maxViewsPerPlugin int
}

func (h *hostService) GetConfig(_ context.Context, req *api.GetConfigRequest) (*api.GetConfigResponse, error) {
	// Runtime overrides set via SetConfig take precedence over static config.
	if req.Key != "" && h.kv != nil {
		if v, ok := h.kv.get(req.PluginName, req.Key); ok {
			return &api.GetConfigResponse{Value: v, Found: true}, nil
		}
	}

	block := h.config[req.PluginName]
	if block == nil {
		return &api.GetConfigResponse{Found: false}, nil
	}

	var target any = block
	if req.Key != "" {
		v, ok := block[req.Key]
		if !ok {
			return &api.GetConfigResponse{Found: false}, nil
		}
		target = v
	}

	data, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	return &api.GetConfigResponse{Value: string(data), Found: true}, nil
}

func (h *hostService) SetConfig(_ context.Context, req *api.SetConfigRequest) (*api.SetConfigResponse, error) {
	if h.kv == nil {
		return &api.SetConfigResponse{}, nil
	}
	if err := h.kv.set(req.PluginName, req.Key, req.Value); err != nil {
		return nil, err
	}
	return &api.SetConfigResponse{}, nil
}

func (h *hostService) GetSessionState(_ context.Context, req *api.GetSessionStateRequest) (*api.GetSessionStateResponse, error) {
	if h.sessionState == nil {
		return &api.GetSessionStateResponse{Found: false}, nil
	}
	state, found := h.sessionState(req.SessionId)
	return &api.GetSessionStateResponse{StateJson: state, Found: found}, nil
}

func (h *hostService) GetAvailableModels(_ context.Context, _ *api.GetAvailableModelsRequest) (*api.GetAvailableModelsResponse, error) {
	if h.models == nil {
		return &api.GetAvailableModelsResponse{}, nil
	}
	return &api.GetAvailableModelsResponse{Models: h.models()}, nil
}

func (h *hostService) Notify(_ context.Context, req *api.NotifyRequest) (*api.NotifyResponse, error) {
	if h.notify != nil {
		h.notify(req.Level, req.Message)
	}
	return &api.NotifyResponse{}, nil
}

func (h *hostService) Confirm(ctx context.Context, req *api.ConfirmRequest) (*api.ConfirmResponse, error) {
	if h.interactivePrompt == nil {
		return &api.ConfirmResponse{Canceled: true}, nil
	}
	confirmed, err := h.interactivePrompt.Confirm(ctx, req.Title, req.Description)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &api.ConfirmResponse{Canceled: true}, nil
		}
		return nil, err
	}
	return &api.ConfirmResponse{Confirmed: confirmed}, nil
}

func (h *hostService) Input(ctx context.Context, req *api.InputRequest) (*api.InputResponse, error) {
	if h.interactivePrompt == nil {
		return &api.InputResponse{Canceled: true}, nil
	}
	value, err := h.interactivePrompt.Input(ctx, req.Title, req.Placeholder)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &api.InputResponse{Canceled: true}, nil
		}
		return nil, err
	}
	return &api.InputResponse{Value: value}, nil
}

func (h *hostService) Log(_ context.Context, req *api.LogRequest) (*api.LogResponse, error) {
	if h.logger == nil || req.Entry == nil {
		return &api.LogResponse{}, nil
	}
	attrs := make([]any, 0, len(req.Entry.Fields)*2)
	for k, v := range req.Entry.Fields {
		attrs = append(attrs, k, v)
	}
	switch req.Entry.Level {
	case "error":
		h.logger.Error(req.Entry.Message, attrs...)
	case "warn":
		h.logger.Warn(req.Entry.Message, attrs...)
	case "debug":
		h.logger.Debug(req.Entry.Message, attrs...)
	default:
		h.logger.Info(req.Entry.Message, attrs...)
	}
	return &api.LogResponse{}, nil
}

func (h *hostService) RenderView(ctx context.Context, req *api.RenderViewRequest) (*api.RenderViewResponse, error) {
	if req.PluginName == "" || req.View == nil || req.View.Id == "" {
		return nil, fmt.Errorf("plugin host: render view: plugin_name and view.id are required")
	}
	if h.viewRenderer == nil {
		return &api.RenderViewResponse{}, nil
	}

	h.viewsMu.Lock()
	if h.views == nil {
		h.views = make(map[string]map[string]struct{})
	}
	owned := h.views[req.PluginName]
	if owned == nil {
		owned = make(map[string]struct{})
		h.views[req.PluginName] = owned
	}
	_, isUpdate := owned[req.View.Id]
	if !isUpdate {
		max := h.maxViewsPerPlugin
		if max <= 0 {
			max = DefaultMaxViewsPerPlugin
		}
		if len(owned) >= max {
			h.viewsMu.Unlock()
			return nil, fmt.Errorf("plugin host: too many open views for plugin %q (max %d)", req.PluginName, max)
		}
		// Reserve the slot atomically with the quota check, before calling
		// the renderer, so two concurrent RenderView calls for distinct new
		// ids can't both pass the check and jointly exceed the quota.
		owned[req.View.Id] = struct{}{}
	}
	h.viewsMu.Unlock()

	if err := h.viewRenderer.RenderView(ctx, req.PluginName, req.View); err != nil {
		if !isUpdate {
			// Roll back the reservation - the render never actually
			// succeeded, so this id must not permanently consume a slot in
			// the plugin's quota for a panel that was never shown.
			h.viewsMu.Lock()
			delete(owned, req.View.Id)
			h.viewsMu.Unlock()
		}
		return nil, err
	}
	return &api.RenderViewResponse{}, nil
}

func (h *hostService) CloseView(ctx context.Context, req *api.CloseViewRequest) (*api.CloseViewResponse, error) {
	if req.PluginName == "" || req.ViewId == "" {
		return nil, fmt.Errorf("plugin host: close view: plugin_name and view_id are required")
	}

	h.viewsMu.Lock()
	owned := h.views[req.PluginName]
	if owned == nil {
		h.viewsMu.Unlock()
		return &api.CloseViewResponse{}, nil
	}
	if _, ok := owned[req.ViewId]; !ok {
		h.viewsMu.Unlock()
		// Not owned by this plugin - silently no-op rather than letting one
		// plugin close another plugin's view via a guessed/colliding id.
		return &api.CloseViewResponse{}, nil
	}
	delete(owned, req.ViewId)
	h.viewsMu.Unlock()

	if h.viewRenderer != nil {
		if err := h.viewRenderer.CloseView(ctx, req.PluginName, req.ViewId); err != nil {
			return nil, err
		}
	}
	return &api.CloseViewResponse{}, nil
}

// closeAllViewsForPlugin closes every view the plugin currently owns and
// clears its registry entry. Called on unload/reload so a killed plugin
// process never leaves stale panels behind.
func (h *hostService) closeAllViewsForPlugin(ctx context.Context, pluginName string) {
	h.viewsMu.Lock()
	owned := h.views[pluginName]
	delete(h.views, pluginName)
	h.viewsMu.Unlock()

	if h.viewRenderer == nil {
		return
	}
	for viewID := range owned {
		if err := h.viewRenderer.CloseView(ctx, pluginName, viewID); err != nil && h.logger != nil {
			h.logger.Warn("plugin host: failed to close view during cleanup", "plugin", pluginName, "view_id", viewID, "err", err)
		}
	}
}

// kvStore is a tiny JSON-file-backed key-value store for plugin SetConfig.
type kvStore struct {
	mu   sync.Mutex
	path string
	data map[string]map[string]string // pluginName -> key -> JSON value
}

func newKVStore(path string) *kvStore {
	s := &kvStore{path: path, data: map[string]map[string]string{}}
	if path == "" {
		return s
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s.data)
	}
	return s
}

func (s *kvStore) get(plugin, key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if block, ok := s.data[plugin]; ok {
		v, ok := block[key]
		return v, ok
	}
	return "", false
}

func (s *kvStore) set(plugin, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[plugin] == nil {
		s.data[plugin] = map[string]string{}
	}
	s.data[plugin][key] = value
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}
