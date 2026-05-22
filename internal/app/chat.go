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

	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/maas"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/pubsub"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/tui"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/tui/notify"
)

// oneShotEventBuffer is the subscriber buffer for runtime events in
// non-interactive (one-shot) mode. Smaller than interactive because
// only a single request/response cycle is processed.
const oneShotEventBuffer = 64

// ChatOptions holds the parameters for launching a chat session.
type ChatOptions struct {
	Endpoint     platform.Endpoint
	Insecure     bool
	MaaSToken    string
	OCPToken     string
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
	maasToken, err := maas.ResolveMaaSToken(ctx, opts.Endpoint, opts.Insecure, opts.MaaSToken, opts.OCPToken, "")
	if err != nil {
		return err
	}

	allModels, discoverErr := maas.DiscoverModels(ctx, opts.Endpoint, maasToken, opts.Insecure)

	// For non-interactive one-shot mode we need a model upfront.
	if !isInteractive(opts.Prompt) {
		if discoverErr != nil {
			return discoverErr
		}
		model, err := pickModel(allModels, opts.Model)
		if err != nil {
			return err
		}
		return runOneShotWithModel(ctx, opts, maasToken, model)
	}

	// Interactive: model discovery failure is non-fatal.
	var model aimchat.ChatModelRef
	if discoverErr == nil {
		model, _ = pickModel(allModels, opts.Model)
	}

	runtime, err := aimchat.NewRuntime(ctx, staticTokenSource(maasToken), aimchat.OpenAIStreamer{Insecure: opts.Insecure})
	if err != nil {
		return err
	}
	defer runtime.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	config := buildSessionConfig(opts, model)

	if err := runtime.Send(aimchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
		return err
	}

	notifyBus := pubsub.New[notify.Notification]()
	defer notifyBus.Close()

	available := buildModelRefs(allModels)

	// Build a refresher closure that captures the endpoint and token so the
	// TUI can re-discover models without importing maas/platform.
	refresher := buildModelRefresher(opts.Endpoint, maasToken, opts.Insecure)

	tuiCfg := tui.Config{
		SessionID:          sessionID,
		ModelName:          model.ID,
		Endpoint:           opts.Endpoint.Key(),
		AvailableModels:    available,
		AvailableEndpoints: endpointKeys(),
		NotifyBus:          notifyBus,
		RefreshModels:      refresher,
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

	return tui.Run(ctx, runtime, tuiCfg)
}

// runOneShotWithModel creates a runtime and runs a one-shot prompt with a
// known-good model.
func runOneShotWithModel(ctx context.Context, opts ChatOptions, maasToken string, model aimchat.ChatModelRef) error {
	runtime, err := aimchat.NewRuntime(ctx, staticTokenSource(maasToken), aimchat.OpenAIStreamer{Insecure: opts.Insecure})
	if err != nil {
		return err
	}
	defer runtime.Close()

	sessionID, err := newID("session")
	if err != nil {
		return err
	}

	config := buildSessionConfig(opts, model)

	if err := runtime.Send(aimchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
		return err
	}

	return runOneShotChat(ctx, runtime, sessionID, opts.Prompt)
}

// buildModelRefresher returns a ModelRefresher closure that re-discovers
// models from MaaS. The closure captures the endpoint and token so the TUI
// doesn't need to import infrastructure packages.
func buildModelRefresher(endpoint platform.Endpoint, maasToken string, insecure bool) tui.ModelRefresher {
	return func(ctx context.Context) ([]tui.ModelInfo, error) {
		models, err := maas.DiscoverModels(ctx, endpoint, maasToken, insecure)
		if err != nil {
			return nil, err
		}
		return buildModelRefs(models), nil
	}
}

func buildSessionConfig(opts ChatOptions, model aimchat.ChatModelRef) aimchat.ChatSessionConfig {
	return aimchat.ChatSessionConfig{
		Endpoint:     opts.Endpoint,
		Model:        model,
		SystemPrompt: opts.SystemPrompt,
		Parameters: aimchat.ChatParameters{
			MaxTokens:   opts.MaxTokens,
			Temperature: opts.Temperature,
		},
	}
}

func runOneShotChat(ctx context.Context, runtime *aimchat.Runtime, sessionID, promptFlag string) error {
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

	if err := runtime.Send(aimchat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   requestID,
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}

	return consumeOneShot(ctx, sub.Channel(), sessionID, requestID)
}

func consumeOneShot(ctx context.Context, events <-chan aimchat.ChatEvent, sessionID, requestID string) error {
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
			case aimchat.ChatResponseDeltaEvent:
				if event.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				fmt.Print(event.Delta)
				wroteOutput = true

			case aimchat.ChatResponseCompletedEvent:
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

			case aimchat.ChatResponseCancelledEvent:
				if event.State.SessionID != sessionID || event.RequestID != requestID {
					continue
				}
				if wroteOutput {
					fmt.Println()
				}
				return errors.New("chat request cancelled")

			case aimchat.ChatRuntimeErrorEvent:
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

func pickModel(models []maas.Model, requestedModel string) (aimchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(aimconfig.LoadConfig().DefaultModel)
	}
	if selectedModel == "" {
		return aimchat.ChatModelRef{}, errors.New("chat model is required")
	}

	available := make([]string, 0, len(models))
	for _, model := range models {
		available = append(available, model.ID)
		if model.ID != selectedModel {
			continue
		}
		if !model.Ready {
			return aimchat.ChatModelRef{}, fmt.Errorf("model %q is not ready", selectedModel)
		}
		return aimchat.ChatModelRef{ID: model.ID, URL: model.URL}, nil
	}

	return aimchat.ChatModelRef{}, fmt.Errorf("model %q not found (available: %s)", selectedModel, strings.Join(available, ", "))
}

func buildModelRefs(models []maas.Model) []tui.ModelInfo {
	refs := make([]tui.ModelInfo, 0, len(models))
	for _, m := range models {
		refs = append(refs, tui.ModelInfo{
			ID:    m.ID,
			URL:   m.URL,
			Ready: m.Ready,
		})
	}
	return refs
}

func endpointKeys() []string {
	keys := make([]string, len(platform.Endpoints))
	for i, ep := range platform.Endpoints {
		keys[i] = ep.Key()
	}
	return keys
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

func staticTokenSource(token string) aimchat.TokenSource {
	trimmed := strings.TrimSpace(token)
	return func(ctx context.Context, endpoint platform.Endpoint) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if trimmed == "" {
			return "", errors.New("MaaS token is required")
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
