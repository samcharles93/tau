package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/app/execute"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/indexing"
	"github.com/samcharles93/tau/internal/metrics"
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

	// UsageTracker: subscribes MetricEvent on this client so the
	// coordinator's metric emission activates. The main event loop
	// subscribes ChatEvent on the same client (see below) so per-client
	// delivery ordering guarantees the tracker has processed
	// llm.response/llm.cost before we snapshot on completion.
	metricsClient := bus.Client("stdin-bridge")
	defer metricsClient.Close()
	tracker := metrics.NewUsageTracker(metricsClient)
	defer tracker.Close()

	// FileSubscriber: when MetricsConfig.Dir is set, append every MetricEvent
	// as JSONL to the configured directory for offline analysis.
	if opts.Config.Metrics.Dir != "" {
		fsClient := bus.Client("stdin-metrics-file")
		fileSub, err := metrics.NewFileSubscriber(fsClient, opts.Config.Metrics.Dir)
		if err != nil {
			slog.Warn("metrics file subscriber unavailable", "err", err)
			fsClient.Close()
		} else {
			defer fileSub.Close()
			defer fsClient.Close()
		}
	}

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

	// Register built-in tools early to extract schemas for the prompt.
	tmpReg := tools.NewRegistry()
	var toolSchemas []tools.Schema
	if err := tools.RegisterBuiltins(tmpReg, cwd, nil); err != nil {
		slog.Warn("registering built-in tools for prompt", "err", err)
	} else {
		toolSchemas = tmpReg.Schemas()
	}
	systemPrompt := buildAgentSystemPrompt(cwd, skillsMgr, toolSchemas)
	workspaceIndex, indexErr := indexing.NewManager(ctx, cwd)
	if indexErr != nil {
		slog.Warn("workspace codesearch unavailable; grep will use direct search", "err", indexErr)
	}

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

	// Drain events via the runner, using the selected renderer.
	runner := execute.NewRunner()
	var renderer execute.Renderer
	if opts.OutputFormat == "jsonl" {
		renderer = execute.NewJSONLRenderer()
	} else {
		renderer = execute.NewPlainRenderer()
	}
	return runner.Run(ctx, chatSub.Events(), renderer, sessionID, tracker)
}
