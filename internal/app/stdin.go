package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
)

const stdInTimeout = 60 * time.Minute

// RunStdIn processes a prompt in non-interactive mode and exits.
func RunStdIn(ctx context.Context, opts ChatOptions, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, stdInTimeout)
	defer cancel()

	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)

	// Models come from the embedded snapshot; no network catalogue load needed.
	var discoverErr error
	allModels, modelsErr := rt.Models(opts.Provider.Name)
	if modelsErr != nil {
		discoverErr = modelsErr
	}

	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.Name, opts.Provider.BaseURL)
	if err != nil {
		return err
	}
	// Headless one-shot has no interactive model picker, so a model is required.
	if strings.TrimSpace(model.ID) == "" {
		return errors.New("chat model is required in non-interactive mode; pass --model or set default_model")
	}

	streamer, err := buildStreamer(ctx, rt, opts.Provider.Name, model)
	if err != nil {
		return fmt.Errorf("building streamer: %w", err)
	}

	cwd, _ := os.Getwd()

	// Create a local event bus and skills manager for headless mode.
	bus := eventbus.New()
	defer bus.Close()

	skillsMgr := skills.NewManager(bus)
	defer skillsMgr.Close()
	extraPaths := append([]string{}, opts.Config.SkillPaths...)
	extraPaths = append(extraPaths, opts.SkillDirs...)
	skillDiscoveryCfg := skills.DiscoveryConfig{
		WorkingDir:     cwd,
		ExtraPaths:     extraPaths,
		DisabledSkills: opts.Config.DisabledSkills,
	}
	if _, err := skillsMgr.Refresh(skillDiscoveryCfg); err != nil {
		slog.Warn("skill discovery failed", "err", err)
	}

	systemPrompt := buildAgentSystemPrompt("", cwd, skillsMgr)

	rawStore, storeErr := sessions.OpenStore()
	var sessionManager *sessions.Manager
	if storeErr != nil {
		slog.Warn("session store unavailable", "err", storeErr)
	} else {
		sessionManager = sessions.NewManager(rawStore)
	}

	coordinator, _, _, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:                   bus,
		ChatOptions:           opts,
		BearerToken:           "",
		SessionManager:        sessionManager,
		InteractiveUI:         false,
		AutoExportJSONL:       false,
		Streamer:              streamer,
		SkillsManager:         skillsMgr,
		SkillsDiscoveryConfig: skillDiscoveryCfg,
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
