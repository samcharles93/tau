package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	aisdkruntime "github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/app/execute"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/indexing"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
)

const stdInTimeout = 60 * time.Minute

// RunStdIn processes a prompt in non-interactive mode and exits.
func RunStdIn(ctx context.Context, opts ChatOptions, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, stdInTimeout)
	defer cancel()

	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)
	model, streamer, discoverErr, err := resolveHeadlessModel(ctx, rt, opts)
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()

	// Create a local event bus and skills manager for headless mode.
	bus := eventbus.New()
	defer bus.Close()

	metricsClient := bus.Client("stdin-bridge")
	tracker, metricsCleanup := setupMetrics(bus, metricsClient, opts.Config.Metrics)
	defer metricsCleanup()
	defer tracker.Close()

	skillsMgr, skillDiscoveryCfg, systemPrompt := initSkillsAndPrompt(ctx, bus, opts, cwd)
	defer skillsMgr.Close()
	workspaceIndex, indexErr := indexing.NewManager(ctx, cwd)
	if indexErr != nil {
		slog.Warn("workspace codesearch unavailable; grep will use direct search", "err", indexErr)
	}

	_, sessionManager, err := openSessionManager(ctx)
	if err != nil {
		slog.Warn("session store unavailable, sessions will not be persisted", "err", err)
	}

	coordinator, err := buildHeadlessCoordinator(ctx, opts, bus, sessionManager, streamer, rt, skillsMgr, skillDiscoveryCfg, workspaceIndex)
	if err != nil {
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

	sessionID, err := tauchat.NewID()
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

	// Subscribe ChatEvent on the same client as the tracker so per-client
	// delivery ordering preserves the publish sequence: completeTurn emits
	// metrics before ChatResponseCompletedEvent, and both arrive in order
	// on this client's dispatch goroutine.
	chatSub := eventbus.Subscribe[tauchat.ChatEvent](metricsClient)
	defer chatSub.Close()

	return submitAndRun(ctx, coordinator, chatSub.Events(), tracker, sessionID, model, opts, prompt)
}

// submitAndRun sends the prompt and drains events via the runner, using the
// selected renderer.
func submitAndRun(ctx context.Context, coordinator *agent.Coordinator, events <-chan tauchat.ChatEvent, tracker *metrics.UsageTracker, sessionID string, model tauchat.ChatModelRef, opts ChatOptions, prompt string) error {
	requestID, err := tauchat.NewID()
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

	runner := execute.NewRunner()
	var renderer execute.Renderer
	if opts.OutputFormat == "jsonl" {
		renderer = execute.NewJSONLRenderer()
	} else {
		renderer = execute.NewPlainRenderer()
	}
	return runner.Run(ctx, events, renderer, sessionID, tracker)
}

// resolveHeadlessModel discovers models from the embedded snapshot, picks the
// launch model (required in headless mode - there is no picker), and builds
// the streamer. Discovery failures are returned as discoverErr for the caller
// to surface.
func resolveHeadlessModel(ctx context.Context, rt *aisdkruntime.Runtime, opts ChatOptions) (tauchat.ChatModelRef, agent.Streamer, error, error) {
	// Models come from the embedded snapshot; no network catalogue load needed.
	var discoverErr error
	allModels, modelsErr := rt.Models(opts.Provider.Name)
	if modelsErr != nil {
		discoverErr = modelsErr
	}

	model, err := pickModel(allModels, opts.Model, opts.Config.DefaultModel, opts.Provider.Name, opts.Provider.BaseURL)
	if err != nil {
		return model, nil, discoverErr, err
	}
	// Headless one-shot has no interactive model picker, so a model is required.
	if strings.TrimSpace(model.ID) == "" {
		return model, nil, discoverErr, errors.New("chat model is required in non-interactive mode; pass --model or set default_model")
	}

	streamer, err := buildStreamer(ctx, rt, opts.Provider.Name, model)
	if err != nil {
		return model, nil, discoverErr, fmt.Errorf("building streamer: %w", err)
	}
	return model, streamer, discoverErr, nil
}

// setupMetrics wires the usage tracker and optional JSONL file subscriber onto
// the bus. The main event loop subscribes ChatEvent on the same client (see
// below) so per-client delivery ordering guarantees the tracker has processed
// llm.response/llm.cost before we snapshot on completion.
func setupMetrics(bus *eventbus.Bus, metricsClient *eventbus.Client, mc tauconfig.MetricsConfig) (*metrics.UsageTracker, func()) {
	tracker := metrics.NewUsageTracker(metricsClient)
	cleanup := func() { metricsClient.Close() }

	// FileSubscriber: when MetricsConfig.Dir is set, append every MetricEvent
	// as JSONL to the configured directory for offline analysis.
	if mc.Dir != "" {
		fsClient := bus.Client("stdin-metrics-file")
		fileSub, err := metrics.NewFileSubscriber(fsClient, mc.Dir)
		if err != nil {
			slog.Warn("metrics file subscriber unavailable", "err", err)
			fsClient.Close()
		} else {
			prev := cleanup
			cleanup = func() {
				fileSub.Close()
				fsClient.Close()
				prev()
			}
		}
	}
	return tracker, cleanup
}

// openSessionManager opens the shared session store, returning nil (and nil
// error) is not allowed - callers warn and proceed without persistence when
// the store cannot be opened.
func openSessionManager(ctx context.Context) (store.SessionStore, *sessions.Manager, error) {
	rawStore, err := sessions.OpenStore(ctx)
	if err != nil {
		return nil, nil, err
	}
	return rawStore, sessions.NewManager(rawStore), nil
}

// buildHeadlessCoordinator creates the coordinator for non-interactive mode.
func buildHeadlessCoordinator(ctx context.Context, opts ChatOptions, bus *eventbus.Bus, sessionManager *sessions.Manager, streamer agent.Streamer, rt *aisdkruntime.Runtime, skillsMgr *skills.Manager, skillDiscoveryCfg skills.DiscoveryConfig, workspaceIndex *indexing.Manager) (*agent.Coordinator, error) {
	coordinator, _, _, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:                   bus,
		ChatOptions:           opts,
		BearerToken:           "",
		SessionManager:        sessionManager,
		InteractiveUI:         false,
		AutoExportJSONL:       false,
		Streamer:              streamer,
		Runtime:               rt,
		SkillsManager:         skillsMgr,
		SkillsDiscoveryConfig: skillDiscoveryCfg,
		MetricsConfig:         opts.Config.Metrics,
		GrepIndex:             workspaceIndex,
	})
	if err != nil {
		if sessionManager != nil {
			if err := sessionManager.Close(); err != nil {
				slog.Warn("closing session store", "err", err)
			}
		}
		return nil, err
	}
	return coordinator, nil
}
