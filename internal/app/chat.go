package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/platform"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/store"
	"github.com/samcharles93/tau/internal/streaming"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// ChatOptions holds the parameters for launching an interactive chat session.
type ChatOptions struct {
	Config          tauconfig.Config
	Provider        tauconfig.ProviderConfig
	Insecure        bool
	Model           string
	SystemPrompt    string
	MaxTokens       int
	Temperature     float64
	Version         string
	ResumeSessionID string // load this session on startup, empty = fresh session
}

// RunChat orchestrates an interactive chat session: resolves tokens and model,
// creates the coordinator, then launches the TUI.
func RunChat(ctx context.Context, opts ChatOptions) error {
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return err
	}

	allModels, discoverErr := provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	// Initialize session store at the RunChat level so it outlives the
	// coordinator (needed for --resume and exit summary).
	sessionsDir := filepath.Join(tauconfig.Dir(), "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		slog.Warn("session store: could not create sessions dir", "err", err)
	}
	storePath := filepath.Join(tauconfig.Dir(), "sessions.db")
	sessionStore, storeErr := store.NewSQLiteStore(storePath, sessionsDir)
	if storeErr != nil {
		slog.Warn("session store unavailable, sessions will not be persisted", "err", storeErr)
		sessionStore = nil
	}

	coordinator, err := newCoordinator(ctx, opts, bearerToken, sessionStore)
	if err != nil {
		if sessionStore != nil {
			sessionStore.Close()
		}
		return err
	}
	defer coordinator.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	// If --resume is set, load the session and use its ID/properties.
	resumeSummary := "" // printed on exit
	if opts.ResumeSessionID != "" && sessionStore != nil {
		resumeID := opts.ResumeSessionID
		if resumeID == "latest" {
			summaries, _, lErr := sessionStore.List(ctx, 1, "")
			if lErr != nil || len(summaries) == 0 {
				return fmt.Errorf("no saved sessions to resume")
			}
			resumeID = summaries[0].ID
		}
		loaded, lErr := sessionStore.Load(ctx, resumeID)
		if lErr != nil {
			return fmt.Errorf("resume session %q: %w", resumeID, lErr)
		}
		sessionID = loaded.SessionID
		// Use the loaded session's model if available.
		if loaded.Model.ID != "" {
			model = loaded.Model
		}
		// Start the session, then load the messages via LoadSessionCommand.
		config := buildSessionConfig(opts, model)
		config.SystemPrompt = loaded.SystemPrompt
		if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
			return err
		}
		// Load the message history into the running session.
		if err := coordinator.Send(tauchat.LoadSessionCommand{SessionID: sessionID}); err != nil {
			return err
		}
		resumeSummary = fmt.Sprintf("Session %s resumed (%d messages). Exit: save + resume with: tau --resume %s",
			sessionID, len(loaded.Messages), sessionID)
	} else {
		config := buildSessionConfig(opts, model)
		if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
			return err
		}
	}

	notifyBus := pubsub.New[notify.Notification]()
	defer notifyBus.Close()

	available := buildModelRefs(allModels)

	// Build a refresher closure that captures the provider and token so the
	// TUI can re-discover models without importing infrastructure packages.
	refresher := buildModelRefresher(opts.Provider, bearerToken, opts.Insecure)

	tuiCfg := tui.Config{
		SessionID:          sessionID,
		ModelName:          model.ID,
		Provider:           opts.Provider.Name,
		AvailableModels:    available,
		AvailableProviders: tauconfig.ProviderNames(opts.Config),
		NotifyBus:          notifyBus,
		RefreshModels:      refresher,
		ShowReasoning:      opts.Config.UI.ShowReasoning,
		Debug:              isDevel(opts.Version, opts.Config),
	}

	// If model discovery failed at startup, notify the user in the TUI
	// rather than refusing to start.
	if discoverErr != nil {
		_ = notifyBus.Publish(ctx, "notifications", notify.Notification{
			Message:  "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:    notify.LevelWarn,
			Duration: 8 * time.Second,
		})
	}

	tuiErr := tui.Run(ctx, coordinator, tuiCfg)

	// Close coordinator first so it can persist sessions while the store is still open.
	coordinator.Close()

	// Print session summary on exit.
	if sessionStore != nil {
		printExitSummary(ctx, sessionStore, sessionID, resumeSummary)
		sessionStore.Close()
	}

	return tuiErr
}

// printExitSummary prints session metadata after the TUI exits.
func printExitSummary(ctx context.Context, s store.SessionStore, sessionID, extra string) {
	summaries, _, err := s.List(ctx, 1, "")
	if err != nil || len(summaries) == 0 {
		return
	}
	// The most recent session is the one we just closed.
	latest := summaries[0]
	if latest.ID != sessionID {
		return
	}

	fmt.Fprintf(os.Stderr, "\nSession %s saved — %d messages, %s tokens",
		latest.ID, latest.MessageCount, formatTokensHuman(latest.TotalTokens))
	if latest.Cost > 0 {
		fmt.Fprintf(os.Stderr, ", $%.4f", latest.Cost)
	}
	fmt.Fprintf(os.Stderr, "\nResume: tau --resume %s\n", latest.ID)
	if extra != "" {
		fmt.Fprintln(os.Stderr, extra)
	}
}

func formatTokensHuman(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// buildModelRefresher returns a ModelRefresher closure that re-discovers
// models from the configured provider. The closure captures the provider and token so the TUI
// does not need to import infrastructure packages.
func buildModelRefresher(selectedProvider tauconfig.ProviderConfig, bearerToken string, insecure bool) tui.ModelRefresher {
	return func(ctx context.Context) ([]tauchat.ChatModelRef, error) {
		models, err := provider.DiscoverModels(ctx, selectedProvider, bearerToken, insecure)
		if err != nil {
			return nil, err
		}
		return buildModelRefs(models), nil
	}
}

func buildSessionConfig(opts ChatOptions, model tauchat.ChatModelRef) tauchat.ChatSessionConfig {
	return tauchat.ChatSessionConfig{
		Provider:     opts.Provider,
		Model:        model,
		SystemPrompt: opts.SystemPrompt,
		Parameters: tauchat.ChatParameters{
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
		},
	}
}

func pickModel(models []provider.Model, requestedModel, defaultModel, baseURL string) (tauchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(defaultModel)
	}
	if selectedModel == "" {
		return tauchat.ChatModelRef{}, errors.New("chat model is required; pass --model or set default_model")
	}

	for _, m := range models {
		if m.ID != selectedModel {
			continue
		}
		if !m.Ready {
			return tauchat.ChatModelRef{}, fmt.Errorf("model %q is not ready", selectedModel)
		}
		return tauchat.ChatModelRef{
			ID:     m.ID,
			URL:    m.URL,
			Ready:  m.Ready,
			Config: m.Config,
		}, nil
	}

	return tauchat.ChatModelRef{ID: selectedModel, URL: strings.TrimRight(baseURL, "/"), Ready: true}, nil
}

func buildModelRefs(models []provider.Model) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		refs = append(refs, tauchat.ChatModelRef{
			ID:     m.ID,
			URL:    m.URL,
			Ready:  m.Ready,
			Config: m.Config,
		})
	}
	return refs
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry, config, and session persistence.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string, sessionStore store.SessionStore) (*agent.Coordinator, error) {
	cwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	if err := tools.RegisterBuiltins(registry, cwd); err != nil {
		return nil, fmt.Errorf("registering built-in tools: %w", err)
	}

	// Plugin manager — discovers and manages extension binaries.
	pluginMgr, err := plugin.NewManager(plugin.Config{
		ToolRegistry: registry,
		Logger:       slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("plugin manager: %w", err)
	}

	registry.SetPluginToolExecutor(func(ctx context.Context, pluginName, toolName string, args json.RawMessage) (tools.Result, error) {
		return pluginMgr.ExecutePluginTool(ctx, pluginName, toolName, args)
	})

	if err := pluginMgr.Load(ctx); err != nil {
		slog.Warn("plugin manager load failed", "err", err)
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		TokenSource:       staticTokenSource(bearerToken),
		Streamer:          streaming.OpenAIStreamer{Insecure: opts.Insecure},
		Registry:          registry,
		ShowReasoning:     opts.Config.UI.ShowReasoning,
		InteractiveUI:     true,
		SessionStore:      sessionStore,
		AutoExportJSONL:   true,
		ExtensionReloader: pluginMgr,
		OnPluginEvent: func(event string, payload *api.EventPayload) *api.EventResponse {
			return pluginMgr.DispatchEvent(ctx, event, payload)
		},
		OnSessionStart: func(eventContext map[string]any) {
			sessionID := eventContext["session_id"].(string)
			pluginMgr.DispatchEvent(ctx, "session_start", &api.EventPayload{
				Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{SessionId: sessionID}},
			})
		},
		OnSessionShutdown: func(eventContext map[string]any) {
			sessionID := eventContext["session_id"].(string)
			pluginMgr.DispatchEvent(ctx, "session_shutdown", &api.EventPayload{
				Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{SessionId: sessionID}},
			})
		},
		OnClose: func() {
			pluginMgr.Unload()
		},
	})
	if err != nil {
		return nil, err
	}
	return coordinator, nil
}

func staticTokenSource(token string) platform.TokenSource {
	trimmed := strings.TrimSpace(token)
	return func(ctx context.Context, _ tauconfig.ProviderConfig) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if trimmed == "" {
			return "", nil
		}
		return trimmed, nil
	}
}

func newID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	suffix := base64.RawURLEncoding.EncodeToString(bytes)
	return prefix + "_" + suffix, nil
}

func isDevel(version string, cfg tauconfig.Config) bool {
	if cfg.Debug {
		return true
	}
	v := strings.ToLower(version)
	return strings.Contains(v, "dev") || strings.Contains(v, "none")
}
