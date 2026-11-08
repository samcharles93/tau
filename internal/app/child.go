package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"

	aisdkruntime "github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
)

// RunChild is the headless child entry point. It writes agent.ready first
// (before reading anything - see step 1 below), reads agent.assign, loads
// its instance and session from the shared store, runs the coordinator
// headless with injected model/tools/limits, and exits after writing
// agent.result on stdout.
// stderr is reserved for log messages only - never protocol.
// Exit codes: 0 after result; 1 for protocol errors; 2 for fatal runtime errors.
func RunChild(ctx context.Context, opts ChatOptions) error {
	stdin := stdio.NewReader(os.Stdin)
	stdout := stdio.NewWriter(os.Stdout)

	assign, err := readAgentAssign(stdout, stdin)
	if err != nil {
		return err
	}

	// Step 3: Open the shared store and validate IDs.	// Step 3: Open the shared store and validate IDs.
	rawStore, storeErr := sessions.OpenStore(ctx)
	if storeErr != nil {
		return fmt.Errorf("open store: %w", storeErr)
	}
	sessionMgr := sessions.NewManager(rawStore)
	defer func() {
		if err := sessionMgr.Close(); err != nil {
			slog.Warn("closing session store", "err", err)
		}
	}()

	// Verify the instance row exists.
	if _, err = rawStore.GetAgentInstance(ctx, assign.InstanceID); err != nil {
		return fmt.Errorf("instance %q not found: %w", assign.InstanceID, err)
	}

	// Step 4: Build the coordinator with the injected model.
	bus := eventbus.New()
	defer bus.Close()

	_, rt, model, streamer, err := resolveChildModel(ctx, opts, assign)
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	skillsMgr := skills.NewManager(bus)
	defer skillsMgr.Close()

	timeout, deadline := parseChildLimits(assign)

	coordinator, _, _, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:                   bus,
		ChatOptions:           opts,
		BearerToken:           "",
		SessionManager:        sessionMgr,
		InteractiveUI:         false,
		AutoExportJSONL:       false,
		Streamer:              streamer,
		Runtime:               rt,
		SkillsManager:         skillsMgr,
		SkillsDiscoveryConfig: skills.DiscoveryConfig{WorkingDir: cwd},
		MetricsConfig:         opts.Config.Metrics,
		MaxTurns:              assign.Limits.MaxTurns,
		Timeout:               timeout,
		MaxTokens:             assign.Budget.MaxTokens,
		Deadline:              deadline,
	})
	if err != nil {
		return fmt.Errorf("build coordinator: %w", err)
	}
	defer coordinator.Close()

	// Apply the injected tool allowlist so buildToolDefs respects it from
	// the first turn. (agent.ready was already written in step 1, before
	// the instance ID was even known - see above.)
	if len(assign.Tools) > 0 {
		coordinator.SetAllowedTools(assign.Tools)
	}

	// Step 5: Subscribe to ChatEvent BEFORE sending any commands.
	// Events published between command dispatch and subscription would
	// be lost because the bus only routes to subscribers registered at
	// publish time (tau-43m).
	chatSub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("child"))
	defer chatSub.Close()

	if err := sendChildStartupCommands(coordinator, opts, model, assign); err != nil {
		return err
	}

	// Step 7: Monitor stdin for cancel/EOF in a background goroutine.
	// The parent may send agent.cancel at any time, or close stdin
	// (parent death / intentional shutdown). Both trigger cancellation.
	go monitorChildStdin(stdin, coordinator, assign.SessionID)

	// Step 8: Stream events until completion, cancellation, or error.
	return runChildEventLoop(ctx, chatSub, stdout, sessionMgr, assign)
}

func childStatus(err string) string {
	if err == "" {
		return string(tauchat.ChildAgentCompleted)
	}
	return string(tauchat.ChildAgentFailed)
}

// ExitChild processes the error from RunChild and calls os.Exit with the
// appropriate code.
func ExitChild(err error) {
	if err == nil {
		os.Exit(0)
	}
	code := 1
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EPIPE) {
		code = 2
	}
	slog.Error("child exiting", "err", err, "exit_code", code)
	os.Exit(code)
}

// readAgentAssign performs the child wire-protocol handshake: it writes
// agent.ready as the first line on stdout (the parent blocks reading
// agent.ready before it will send agent.assign, so reading first deadlocked
// unconditionally), then reads and parses agent.assign. The real instance ID
// isn't known yet - the parent doesn't validate the ready field.
func readAgentAssign(stdout *stdio.Writer, stdin *stdio.Reader) (bridge.AgentAssign, error) {
	// Step 1: Write agent.ready as the first line on stdout - this MUST
	// come before reading anything from stdin (docs/specs/agents/
	// 03-wire-protocol.md: "The child writes agent.ready as its first
	// line; the parent replies with agent.assign").
	if err := stdout.WriteHandshake("", os.Getpid()); err != nil {
		return bridge.AgentAssign{}, fmt.Errorf("write agent.ready: %w", err)
	}

	// Step 2: Read agent.assign from parent.
	typ, payload, err := stdin.ReadEnvelope()
	if err != nil {
		return bridge.AgentAssign{}, fmt.Errorf("read agent.assign: %w", err)
	}
	if typ != "agent.assign" {
		return bridge.AgentAssign{}, fmt.Errorf("expected agent.assign, got %q", typ)
	}
	var assign bridge.AgentAssign
	if err := json.Unmarshal(payload, &assign); err != nil {
		return bridge.AgentAssign{}, fmt.Errorf("unmarshal agent.assign: %w", err)
	}
	return assign, nil
}

// parseChildLimits resolves the child's time limits. budget.timeout (a
// spawn-call override) takes precedence over the structural limits.timeout
// (spec/config default) when both are present, per
// docs/specs/agents/02-spawning-and-lifecycle.md (Time-based limits).
func parseChildLimits(assign bridge.AgentAssign) (timeout time.Duration, deadline time.Time) {
	if assign.Budget.Timeout != "" {
		timeout, _ = time.ParseDuration(assign.Budget.Timeout)
	} else if assign.Limits.Timeout != "" {
		timeout, _ = time.ParseDuration(assign.Limits.Timeout)
	}
	if assign.Budget.Deadline != "" {
		deadline, _ = time.Parse(time.RFC3339, assign.Budget.Deadline)
	}
	return timeout, deadline
}

// sendChildStartupCommands starts the session, loads persisted history, and
// submits the assigned prompt.
func sendChildStartupCommands(coordinator *agent.Coordinator, opts ChatOptions, model tauchat.ChatModelRef, assign bridge.AgentAssign) error {
	cfg := buildSessionConfig(opts, model, "")
	if err := coordinator.Send(tauchat.StartChatSessionCommand{
		SessionID: assign.SessionID,
		Config:    cfg,
	}); err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	if err := coordinator.Send(tauchat.LoadSessionCommand{
		SessionID:        assign.SessionID,
		RuntimeSessionID: assign.SessionID,
	}); err != nil {
		return fmt.Errorf("load session: %w", err)
	}

	requestID, _ := tauchat.NewID()
	if err := coordinator.Send(tauchat.SubmitChatPromptCommand{
		SessionID:   assign.SessionID,
		RequestID:   requestID,
		Prompt:      assign.Prompt,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("submit prompt: %w", err)
	}
	return nil
}

// monitorChildStdin watches stdin for agent.cancel or EOF (parent death /
// intentional shutdown) and cancels the running turn either way. A direct
// CancelChatRequestCommand is sent rather than closing a channel fed into a
// select - a closed channel is permanently readable in Go and would cause
// repeated cancellations (tau-43m).
func monitorChildStdin(stdin *stdio.Reader, coordinator *agent.Coordinator, sessionID string) {
	for {
		typ, _, err := stdin.ReadEnvelope()
		if err != nil {
			// stdin EOF: parent died or closed the pipe.
			slog.Info("child: stdin closed, treating as cancel", "err", err)
			_ = coordinator.Send(tauchat.CancelChatRequestCommand{
				SessionID: sessionID,
			})
			return
		}
		if typ == "agent.cancel" {
			slog.Info("child: received agent.cancel")
			_ = coordinator.Send(tauchat.CancelChatRequestCommand{
				SessionID: sessionID,
			})
			return
		}
	}
}

// runChildEventLoop streams ChatEvents until completion, cancellation, or
// error, writing agent.usage / agent.result envelopes on stdout as the wire
// protocol requires. Turns come from ChatResponseCompletedEvent.TurnIterations
// (the authoritative model-iteration count from the coordinator). Token
// totals use ChatResponseCompletedEvent.CumulativeUsage (aggregate across all
// iterations) rather than State.LastUsage (last call only). Both fixes
// address tau-47m / tau-47k.
func runChildEventLoop(ctx context.Context, chatSub *eventbus.Subscriber[tauchat.ChatEvent], stdout *stdio.Writer, sessionMgr *sessions.Manager, assign bridge.AgentAssign) error {
	var acc childResultAccum

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - exit without writing result.
			return fmt.Errorf("context cancelled: %w", ctx.Err())

		case event, ok := <-chatSub.Events():
			if !ok {
				return errors.New("event stream closed unexpectedly")
			}
			switch e := event.(type) {
			case tauchat.ChatResponseDeltaEvent:
				if e.SessionID == assign.SessionID {
					acc.finalText.WriteString(e.Delta)
				}

			case tauchat.ChatResponseCompletedEvent:
				if e.State.SessionID != assign.SessionID {
					continue
				}
				return handleChildCompleted(ctx, stdout, sessionMgr, assign, e, &acc)

			case tauchat.ChatRuntimeErrorEvent:
				if e.SessionID != assign.SessionID {
					continue
				}
				return handleChildError(stdout, assign, e, &acc)

			case tauchat.ChatResponseCancelledEvent:
				if e.State.SessionID != assign.SessionID {
					continue
				}
				return handleChildCancelled(ctx, stdout, sessionMgr, assign, e, &acc)
			}
		}
	}
}

// childResultAccum carries the mutable per-run state of the child event loop.
type childResultAccum struct {
	cumulativeTurns   int
	totalInputTokens  int
	totalOutputTokens int
	totalCost         float64
	finalText         strings.Builder
	lastError         string
}

// usageEnvelope builds the agent.usage envelope from the accumulated totals.
func (a *childResultAccum) usageEnvelope(instance string) bridge.AgentUsage {
	return bridge.AgentUsage{
		Instance:     instance,
		Turns:        a.cumulativeTurns,
		InputTokens:  a.totalInputTokens,
		OutputTokens: a.totalOutputTokens,
		Cost:         a.totalCost,
	}
}

// resultEnvelope builds an agent.result envelope from the accumulated totals.
func (a *childResultAccum) resultEnvelope(assign bridge.AgentAssign, status, errMsg string, partial bool) bridge.AgentResult {
	return bridge.AgentResult{
		TaskID:    assign.TaskID,
		Status:    status,
		FinalText: a.finalText.String(),
		SessionID: assign.SessionID,
		Usage: bridge.AgentResultUsage{
			Turns:        a.cumulativeTurns,
			InputTokens:  a.totalInputTokens,
			OutputTokens: a.totalOutputTokens,
			Cost:         a.totalCost,
		},
		Error:   errMsg,
		Partial: partial,
	}
}

// handleChildCompleted processes a completed turn: it accumulates usage from
// the authoritative event fields, emits agent.usage with cumulative totals
// per the wire spec, persists, writes agent.result, and returns nil.
func handleChildCompleted(ctx context.Context, stdout *stdio.Writer, sessionMgr *sessions.Manager, assign bridge.AgentAssign, e tauchat.ChatResponseCompletedEvent, acc *childResultAccum) error {
	if e.State.LastError != "" {
		acc.lastError = e.State.LastError
	}
	status := childStatus(acc.lastError)
	if e.BudgetExhausted {
		status = string(tauchat.ChildAgentBudgetExhausted)
	}
	if e.TimedOut {
		status = string(tauchat.ChildAgentTimedOut)
	}

	// Accumulate usage from the authoritative event fields.
	acc.cumulativeTurns += e.TurnIterations
	acc.totalInputTokens = e.CumulativeUsage.PromptTokens
	acc.totalOutputTokens = e.CumulativeUsage.CompletionTokens
	acc.totalCost = e.CumulativeCost

	// Emit agent.usage with cumulative totals per wire spec.
	_ = stdout.WriteEnvelope("agent.usage", acc.usageEnvelope(assign.InstanceID))

	// Always persist before sending result.
	if sessionMgr != nil {
		_ = sessionMgr.Save(ctx, e.State, 0)
	}
	if err := stdout.WriteEnvelope("agent.result", acc.resultEnvelope(assign, status, acc.lastError, e.Partial)); err != nil {
		return fmt.Errorf("write agent.result: %w", err)
	}
	return nil
}

// handleChildError writes a failed agent.result envelope and returns the
// runtime error.
func handleChildError(stdout *stdio.Writer, assign bridge.AgentAssign, e tauchat.ChatRuntimeErrorEvent, acc *childResultAccum) error {
	_ = stdout.WriteEnvelope("agent.result", acc.resultEnvelope(assign, string(tauchat.ChildAgentFailed), e.Message, true))
	return fmt.Errorf("runtime error: %s", e.Message)
}

// handleChildCancelled persists, writes a cancelled agent.result envelope, and
// returns nil.
func handleChildCancelled(ctx context.Context, stdout *stdio.Writer, sessionMgr *sessions.Manager, assign bridge.AgentAssign, e tauchat.ChatResponseCancelledEvent, acc *childResultAccum) error {
	// Always persist before sending result.
	if sessionMgr != nil {
		_ = sessionMgr.Save(ctx, e.State, 0)
	}
	_ = stdout.WriteEnvelope("agent.result", acc.resultEnvelope(assign, string(tauchat.ChildAgentCancelled), "", true))
	return nil
}

// resolveChildModel builds the provider (falling back to the assigned model's
// provider), the runtime, the resolved model, and the streamer for the child.
func resolveChildModel(ctx context.Context, opts ChatOptions, assign bridge.AgentAssign) (tauconfig.ProviderConfig, *aisdkruntime.Runtime, tauchat.ChatModelRef, agent.Streamer, error) {
	provider := opts.Provider
	if provider.Name == "" {
		provider.Name = assign.Model.Provider
	}

	rt := newRuntimeForProvider(provider, opts.Insecure)
	allModels, _ := rt.Models(provider.Name)
	model, err := pickModel(allModels, assign.Model.Model, "", provider.Name, provider.BaseURL)
	if err != nil {
		return provider, nil, model, nil, fmt.Errorf("resolve model: %w", err)
	}
	if model.ID == "" {
		model.ID = assign.Model.Model
	}

	streamer, err := buildStreamer(ctx, rt, provider.Name, model)
	if err != nil {
		return provider, nil, model, nil, fmt.Errorf("build streamer: %w", err)
	}
	return provider, rt, model, streamer, nil
}
