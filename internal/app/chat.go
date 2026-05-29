package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/extensions"
	"github.com/samcharles93/tau/internal/platform"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/pubsub"
	"github.com/samcharles93/tau/internal/streaming"
	"github.com/samcharles93/tau/internal/tui"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// oneShotEventBuffer is the subscriber buffer for runtime events in
// non-interactive (one-shot) mode. Smaller than interactive because
// only a single request/response cycle is processed.
const oneShotEventBuffer = 64

// ChatOptions holds the parameters for launching a chat session.
type ChatOptions struct {
	Config       tauconfig.Config
	Provider     tauconfig.ProviderConfig
	Insecure     bool
	Model        string
	SystemPrompt string
	MaxTokens    int
	Temperature  float64
	Prompt       string
}

// RunChat orchestrates a chat session: resolves tokens and model, creates
// the runtime, then launches either the interactive TUI or a one-shot stream
// depending on whether a prompt was provided.
//
// For interactive sessions, model discovery failure at startup is non-fatal:
// the TUI opens with an empty model list and the user can retry via /models.
// For one-shot mode a working model is required upfront.
func RunChat(ctx context.Context, opts ChatOptions) error {
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return err
	}

	if !isInteractive(opts.Prompt) {
		model, err := pickModel(provider.ConfiguredModels(opts.Provider), opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
		if err != nil {
			return err
		}
		return runOneShotWithModel(ctx, opts, bearerToken, model)
	}

	allModels, discoverErr := provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	coordinator, err := newCoordinator(ctx, opts, bearerToken, true)
	if err != nil {
		return err
	}
	defer coordinator.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	config := buildSessionConfig(opts, model)

	if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
		return err
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
	}

	// If model discovery failed at startup, notify the user in the TUI
	// rather than refusing to start.
	if discoverErr != nil {
		_ = notifyBus.Publish(ctx, "notifications", notify.Notification{
			Message:  "Model discovery failed: " + discoverErr.Error() + " (`/refresh` to retry)",
			Level:    notify.LevelWarn,
			Duration: 8 * time.Second, // longer for startup warnings
		})
	}

	return tui.Run(ctx, coordinator, tuiCfg)
}

// runOneShotWithModel creates a coordinator and runs a one-shot prompt with a
// known-good model.
func runOneShotWithModel(ctx context.Context, opts ChatOptions, bearerToken string, model tauchat.ChatModelRef) error {
	coordinator, err := newCoordinator(ctx, opts, bearerToken, false)
	if err != nil {
		return err
	}
	defer coordinator.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	config := buildSessionConfig(opts, model)

	if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
		return err
	}

	return runOneShotChat(ctx, coordinator, sessionID, opts.Prompt, opts.Config.UI.ShowReasoning)
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

func runOneShotChat(ctx context.Context, runtime tauchat.ChatRuntime, sessionID, promptFlag string, showReasoning bool) error {
	prompt, err := resolvePrompt(promptFlag)
	if err != nil {
		return err
	}

	sub, err := runtime.SubscribeEvents(oneShotEventBuffer)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	requestID, err := newID("request")
	if err != nil {
		return err
	}

	if err := runtime.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   requestID,
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}

	return consumeOneShot(ctx, sub.Channel(), sessionID, requestID, showReasoning)
}

func consumeOneShot(ctx context.Context, events <-chan tauchat.ChatEvent, sessionID, requestID string, showReasoning bool) error {
	wroteOutput := false

	for {
		select {
		case <-ctx.Done():
			if wroteOutput {
				fmt.Println()
			}
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return errors.New("chat runtime stopped before the request completed")
			}
			switch event := event.(type) {
			case tauchat.ChatResponseDeltaEvent:
				if event.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				fmt.Print(event.Delta)
				wroteOutput = true

			case tauchat.ChatReasoningDeltaEvent:
				if !showReasoning || event.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				fmt.Fprint(os.Stderr, event.Delta)

			case tauchat.ChatToolExecutionStartedEvent:
				if event.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				fmt.Fprintf(os.Stderr, "\ntool started: %s %s\n", event.ToolName, event.ArgumentsSummary)

			case tauchat.ChatToolExecutionCompletedEvent:
				if event.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				fmt.Fprintf(os.Stderr, "tool completed: %s %s (%s)\n", event.ToolName, event.Status, event.Duration)

			case tauchat.ChatResponseCompletedEvent:
				if event.State.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				if wroteOutput {
					fmt.Println()
				}
				if event.FinishReason == "length" {
					fmt.Fprintln(os.Stderr, "warning: response hit max_tokens and may be truncated")
				}
				return nil

			case tauchat.ChatResponseCancelledEvent:
				if event.State.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				if wroteOutput {
					fmt.Println()
				}
				return errors.New("chat request cancelled")

			case tauchat.ChatRuntimeErrorEvent:
				if event.SessionID != sessionID {
					continue
				}
				if event.RequestID != "" && event.RequestID != requestID {
					continue
				}
				return errors.New(event.Message)
			}
		}
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

	for _, model := range models {
		if model.ID != selectedModel {
			continue
		}
		if !model.Ready {
			return tauchat.ChatModelRef{}, fmt.Errorf("model %q is not ready", selectedModel)
		}
		return tauchat.ChatModelRef{
			ID:     model.ID,
			URL:    model.URL,
			Ready:  model.Ready,
			Config: model.Config,
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

func resolvePrompt(flagValue string) (string, error) {
	prompt := strings.TrimSpace(flagValue)
	if prompt != "" {
		return prompt, nil
	}

	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		prompt = strings.TrimSpace(string(data))
		if prompt == "" {
			return "", errors.New("stdin prompt was empty")
		}
		return prompt, nil
	}

	return "", errors.New("prompt is required; pass --prompt or pipe input")
}

func isInteractive(promptFlag string) bool {
	if strings.TrimSpace(promptFlag) != "" {
		return false
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// newCoordinator creates and returns an agent coordinator with the standard
// tool registry and config. Both interactive and one-shot paths use this.
func newCoordinator(ctx context.Context, opts ChatOptions, bearerToken string, interactive bool) (*agent.Coordinator, error) {
	cwd, _ := os.Getwd()
	registry := tools.NewRegistry()
	if err := tools.RegisterBuiltins(registry, cwd); err != nil {
		return nil, fmt.Errorf("registering built-in tools: %w", err)
	}
	extensionManager, err := extensions.NewManager(extensions.Config{
		WorkingDir:       cwd,
		Sources:          extensions.SourcesFromConfig(cwd, opts.Config.Extensions),
		Disabled:         opts.Config.Extensions.Disabled,
		ReservedCommands: tui.BuiltinCommandNames(),
		Registry:         registry,
	})
	if err != nil {
		return nil, fmt.Errorf("creating extension manager: %w", err)
	}
	if err := extensionManager.Load(ctx); err != nil {
		return nil, fmt.Errorf("loading extensions: %w", err)
	}

	coordinator, err := agent.NewCoordinator(ctx, agent.CoordinatorConfig{
		TokenSource:   staticTokenSource(bearerToken),
		Streamer:      streaming.OpenAIStreamer{Insecure: opts.Insecure},
		Registry:      registry,
		ShowReasoning: opts.Config.UI.ShowReasoning,
		InteractiveUI: interactive,
		ExtensionReloader: extensionReloader{
			manager: extensionManager,
		},
		OnSessionStart: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventSessionStart, eventContext)
		},
		OnSessionShutdown: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventSessionShutdown, eventContext)
		},
		OnToolStarted: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventToolCallStarted, eventContext)
		},
		OnToolCompleted: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventToolCallCompleted, eventContext)
		},
		OnReasoningDelta: func(eventContext map[string]any) {
			extensionManager.Dispatch(extensions.EventReasoningDelta, eventContext)
		},
		OnClose:       extensionManager.Unload,
		StartupEvents: extensionStartupEvents(extensionManager.Snapshot()),
	})
	if err != nil {
		extensionManager.Unload()
		return nil, err
	}
	return coordinator, nil
}

func extensionStartupEvents(snapshot extensions.Snapshot) []tauchat.ChatEvent {
	events := []tauchat.ChatEvent{tauchat.ExtensionCommandsChangedEvent{
		Commands:   extensionCommands(snapshot.Commands),
		OccurredAt: time.Now().UTC(),
	}}
	for _, diagnostic := range snapshot.Diagnostics {
		if diagnostic.Severity != extensions.SeverityError {
			continue
		}
		message := "Extension error"
		if diagnostic.ExtensionName != "" {
			message += " (" + diagnostic.ExtensionName + ")"
		}
		message += ": " + diagnostic.Message
		events = append(events, tauchat.ChatRuntimeErrorEvent{
			Message:    message,
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
	}
	return events
}

type extensionReloader struct {
	manager *extensions.Manager
}

func (r extensionReloader) ReloadExtensions(ctx context.Context, idle bool) (tauchat.ExtensionReloadResult, error) {
	if r.manager == nil {
		return tauchat.ExtensionReloadResult{}, errors.New("extension manager is not available")
	}
	if err := r.manager.ReloadIfIdle(ctx, idle); err != nil {
		return tauchat.ExtensionReloadResult{}, err
	}
	snapshot := r.manager.Snapshot()
	return tauchat.ExtensionReloadResult{
		ExtensionCount: len(snapshot.Extensions),
		Diagnostics:    extensionDiagnostics(snapshot.Diagnostics),
		Commands:       extensionCommands(snapshot.Commands),
	}, nil
}

func (r extensionReloader) ExtensionCommands() []tauchat.ExtensionCommand {
	if r.manager == nil {
		return nil
	}
	return extensionCommands(r.manager.Snapshot().Commands)
}

func (r extensionReloader) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error) {
	if r.manager == nil {
		return "", errors.New("extension manager is not available")
	}
	ui, _ := uiBridge.(tools.UIBridge)
	return r.manager.ExecuteCommand(ctx, name, args, ui)
}

func extensionDiagnostics(diagnostics []extensions.Diagnostic) []tauchat.ExtensionDiagnostic {
	out := make([]tauchat.ExtensionDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, tauchat.ExtensionDiagnostic{
			Path:          diagnostic.Path,
			ExtensionName: diagnostic.ExtensionName,
			Severity:      string(diagnostic.Severity),
			Message:       diagnostic.Message,
		})
	}
	return out
}

func extensionCommands(commands []extensions.Command) []tauchat.ExtensionCommand {
	out := make([]tauchat.ExtensionCommand, 0, len(commands))
	for _, command := range commands {
		out = append(out, tauchat.ExtensionCommand{
			Name:          command.Name,
			Description:   command.Description,
			ExtensionName: command.ExtensionName,
		})
	}
	return out
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
