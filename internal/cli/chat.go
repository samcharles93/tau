package cli

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
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/tui"
	urfavecli "github.com/urfave/cli/v3"
)

var errEmptyChatTokenSource = errors.New("MaaS token is required")

var defaultChatParameters = aimchat.DefaultParameters()

func chatCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "chat",
		Usage: "Stream a prompt against a MaaS chat model",
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{
				Name:  "prompt",
				Usage: "Prompt text to send; if omitted, stdin is used when piped",
			},
			&urfavecli.StringFlag{
				Name:  "model",
				Usage: "Model ID to use for chat",
			},
			&urfavecli.StringFlag{
				Name:  "system-prompt",
				Usage: "Override the system prompt for this chat session",
			},
			&urfavecli.IntFlag{
				Name:  "max-tokens",
				Usage: "Maximum completion tokens per response",
				Value: defaultChatParameters.MaxTokens,
			},
			&urfavecli.FloatFlag{
				Name:  "temperature",
				Usage: "Sampling temperature for the model response",
				Value: defaultChatParameters.Temperature,
			},
			&urfavecli.StringFlag{
				Name:  "ocp-token",
				Usage: "Skip OAuth and use a pre-existing OCP token",
			},
			&urfavecli.StringFlag{
				Name:  "maas-token",
				Usage: "Skip OAuth+exchange and use a pre-existing MaaS token",
			},
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			endpoint, err := platform.ResolveEndpoint(cmd.Root().String("endpoint"))
			if err != nil {
				return err
			}
			insecure := cmd.Root().Bool("insecure")

			maasToken, err := maas.ResolveMaaSToken(ctx, endpoint, insecure, cmd.String("maas-token"), cmd.String("ocp-token"), "")
			if err != nil {
				return err
			}

			model, err := resolveChatModel(ctx, endpoint, maasToken, cmd.String("model"), insecure)
			if err != nil {
				return err
			}

			runtime, err := aimchat.NewRuntime(staticChatTokenSource(maasToken), aimchat.OpenAIStreamer{Insecure: insecure})
			if err != nil {
				return err
			}
			defer runtime.Close()

			sessionID, err := newChatID("session")
			if err != nil {
				return err
			}

			config := aimchat.ChatSessionConfig{
				Endpoint:     endpoint,
				Model:        model,
				SystemPrompt: cmd.String("system-prompt"),
				Parameters: aimchat.ChatParameters{
					MaxTokens:   cmd.Int("max-tokens"),
					Temperature: cmd.Float("temperature"),
				},
			}

			if err := runtime.Send(aimchat.StartChatSessionCommand{SessionID: sessionID, Config: config}); err != nil {
				return err
			}

			// Interactive TUI mode: launch when no --prompt and stdin is a terminal.
			if isInteractiveChat(cmd.String("prompt")) {
				return tui.Run(runtime, tui.Config{
					SessionID: sessionID,
					ModelName: model.ID,
					Endpoint:  endpoint.Key(),
				})
			}

			// One-shot mode: resolve prompt and stream a single response.
			prompt, err := resolveChatPrompt(cmd.String("prompt"))
			if err != nil {
				return err
			}

			sub, err := runtime.SubscribeEvents(64)
			if err != nil {
				return err
			}
			defer sub.Unsubscribe()

			requestID, err := newChatID("request")
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

			return consumeOneShotChat(sub.Channel(), sessionID, requestID)
		},
	}
}

func resolveChatPrompt(flagValue string) (string, error) {
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

// isInteractiveChat returns true when no explicit prompt was provided
// and stdin is a terminal — the conditions for launching the TUI.
func isInteractiveChat(promptFlag string) bool {
	if strings.TrimSpace(promptFlag) != "" {
		return false
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func resolveChatModel(ctx context.Context, endpoint platform.Endpoint, maasToken, requestedModel string, insecure bool) (aimchat.ChatModelRef, error) {
	selectedModel := strings.TrimSpace(requestedModel)
	if selectedModel == "" {
		selectedModel = strings.TrimSpace(aimconfig.LoadConfig().DefaultModel)
	}
	if selectedModel == "" {
		return aimchat.ChatModelRef{}, errors.New("chat model is required")
	}

	models, err := maas.DiscoverModels(ctx, endpoint, maasToken, insecure)
	if err != nil {
		return aimchat.ChatModelRef{}, err
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

func consumeOneShotChat(events <-chan aimchat.ChatEvent, sessionID, requestID string) error {
	wroteOutput := false

	for event := range events {
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

	return errors.New("chat runtime stopped before the request completed")
}

func staticChatTokenSource(token string) aimchat.TokenSource {
	trimmed := strings.TrimSpace(token)
	return func(ctx context.Context, endpoint platform.Endpoint) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if trimmed == "" {
			return "", errEmptyChatTokenSource
		}
		return trimmed, nil
	}
}

func newChatID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating %s id: %w", prefix, err)
	}
	suffix := base64.RawURLEncoding.EncodeToString(bytes)
	return prefix + "_" + suffix, nil
}
