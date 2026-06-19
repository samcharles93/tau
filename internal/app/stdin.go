package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/provider"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/pkg/ai"
)

const stdInTimeout = 60 * time.Minute

// RunStdIn processes a prompt in non-interactive mode and exits.
func RunStdIn(ctx context.Context, opts ChatOptions, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, stdInTimeout)
	defer cancel()

	// TODO: This makes no sense
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return err
	}

	allModels, discoverErr := provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.BaseURL)
	if err != nil {
		return err
	}

	catalog := ai.NewCatalog(ai.DefaultCatalogOptions(opts.Insecure))
	if catalogErr := catalog.Load(ctx); catalogErr != nil {
		if discoverErr == nil {
			discoverErr = catalogErr
		}
		slog.Warn("model catalog load failed", "err", catalogErr)
	}

	streamer, err := buildStreamer(opts.Provider, model, bearerToken, catalog, opts.Insecure)
	if err != nil {
		slog.Warn("ai-sdk streamer unavailable; falling back to OpenAI-compatible streamer", "err", err)
		streamer = provider.OpenAIStreamer{Insecure: opts.Insecure}
	}

	cwd, _ := os.Getwd()
	systemPrompt := buildAgentSystemPrompt("", cwd)

	rawStore, storeErr := sessions.OpenStore()
	var sessionManager *sessions.Manager
	if storeErr != nil {
		slog.Warn("session store unavailable", "err", storeErr)
	} else {
		sessionManager = sessions.NewManager(rawStore)
	}

	coordinator, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:             eventbus.New(),
		ChatOptions:     opts,
		BearerToken:     bearerToken,
		SessionManager:  sessionManager,
		InteractiveUI:   false,
		AutoExportJSONL: false,
		Streamer:        streamer,
	})
	if err != nil {
		if sessionManager != nil {
			if err := sessionManager.Close(); err != nil {
				slog.Warn("closing session store", "err", err)
			}
		}
		return err
	}
	defer func() {
		coordinator.Close()
		if sessionManager != nil {
			if err := sessionManager.Close(); err != nil {
				slog.Warn("closing session store", "err", err)
			}
		}
	}()

	sessionID, err := newID()
	if err != nil {
		return err
	}

	cfg := buildSessionConfig(opts, model, systemPrompt)
	if err := coordinator.Send(tauchat.StartChatSessionCommand{SessionID: sessionID, Config: cfg}); err != nil {
		return err
	}

	if discoverErr != nil {
		slog.Warn("model discovery failed", "err", discoverErr)
	}

	sub, err := coordinator.SubscribeEvents()
	if err != nil {
		return err
	}
	defer sub.Close()

	requestID, err := newID()
	if err != nil {
		return err
	}

	slog.Info("single-shot: sending prompt", "model", model.ID, "provider", opts.Provider.Name)
	if err := coordinator.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   sessionID,
		RequestID:   requestID,
		Prompt:      prompt,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}

	// Wait for completion, cancellation, or timeout.
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\ntimed out")
			return ctx.Err()
		case event, ok := <-sub.Events():
			if !ok {
				return nil
			}
			switch e := event.(type) {
			case tauchat.ChatResponseDeltaEvent:
				if e.SessionID == sessionID {
					fmt.Print(e.Delta)
				}
			case tauchat.ChatResponseCompletedEvent:
				if e.State.SessionID == sessionID {
					fmt.Println()
					if e.State.LastError != "" {
						fmt.Fprintf(os.Stderr, "\nerror: %s\n", e.State.LastError)
					}
					if e.FinishReason == "length" {
						fmt.Fprintln(os.Stderr, "\nwarning: response was truncated by max_tokens; rerun with --max-tokens N for a longer answer")
					}
					return nil
				}
			case tauchat.ChatRuntimeErrorEvent:
				if e.SessionID == sessionID {
					return fmt.Errorf("%s", e.Message)
				}
			case tauchat.ChatResponseCancelledEvent:
				if e.State.SessionID == sessionID {
					fmt.Fprintln(os.Stderr, "\ncancelled")
					return nil
				}
			}
		}
	}
}
