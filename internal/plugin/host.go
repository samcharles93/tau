package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samcharles93/tau/pkg/plugin/api"
)

// Notifier pushes a user-visible notification to the host UI (TUI + web).
type Notifier func(level, message string)

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
	notify       Notifier
	models       func() []string
	sessionState func(sessionID string) (stateJSON string, found bool)
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
