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

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
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

	// Step 1: Write agent.ready as the first line on stdout - this MUST
	// come before reading anything from stdin. The parent blocks reading
	// agent.ready before it will send agent.assign (docs/specs/agents/
	// 03-wire-protocol.md: "The child writes agent.ready as its first
	// line; the parent replies with agent.assign"); reading assign first
	// here deadlocked unconditionally, both sides waiting on each other.
	// The real instance ID isn't known yet (the parent assigns it via
	// agent.assign, next) - the parent doesn't validate this field.
	if err := stdout.WriteHandshake("", os.Getpid()); err != nil {
		return fmt.Errorf("write agent.ready: %w", err)
	}

	// Step 2: Read agent.assign from parent.
	typ, payload, err := stdin.ReadEnvelope()
	if err != nil {
		return fmt.Errorf("read agent.assign: %w", err)
	}
	if typ != "agent.assign" {
		return fmt.Errorf("expected agent.assign, got %q", typ)
	}
	var assign bridge.AgentAssign
	if err := json.Unmarshal(payload, &assign); err != nil {
		return fmt.Errorf("unmarshal agent.assign: %w", err)
	}

	// Step 3: Open the shared store and validate IDs.
	rawStore, storeErr := sessions.OpenStore()
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

	provider := opts.Provider
	if provider.Name == "" {
		provider.Name = assign.Model.Provider
	}

	rt := newRuntimeForProvider(provider, opts.Insecure)
	allModels, _ := rt.Models(provider.Name)
	model, err := pickModel(allModels, assign.Model.Model, "", provider.Name, provider.BaseURL)
	if err != nil {
		return fmt.Errorf("resolve model: %w", err)
	}
	if model.ID == "" {
		model.ID = assign.Model.Model
	}

	streamer, err := buildStreamer(ctx, rt, provider.Name, model)
	if err != nil {
		return fmt.Errorf("build streamer: %w", err)
	}

	cwd, _ := os.Getwd()
	skillsMgr := skills.NewManager(bus)
	defer skillsMgr.Close()

	// Parse limits before building the coordinator. budget.timeout (a
	// spawn-call override) takes precedence over the structural
	// limits.timeout (spec/config default) when both are present, per
	// docs/specs/agents/02-spawning-and-lifecycle.md (Time-based limits).
	var timeout time.Duration
	var deadline time.Time
	if assign.Budget.Timeout != "" {
		timeout, _ = time.ParseDuration(assign.Budget.Timeout)
	} else if assign.Limits.Timeout != "" {
		timeout, _ = time.ParseDuration(assign.Limits.Timeout)
	}
	if assign.Budget.Deadline != "" {
		deadline, _ = time.Parse(time.RFC3339, assign.Budget.Deadline)
	}

	coordinator, _, _, err := buildCoordinator(ctx, coordinatorConfig{
		Bus:                   bus,
		ChatOptions:           opts,
		BearerToken:           "",
		SessionManager:        sessionMgr,
		InteractiveUI:         false,
		AutoExportJSONL:       false,
		Streamer:              streamer,
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

	// Step 5: Start session, load persisted history, submit prompt.
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

	// Step 6: Monitor stdin for cancel/EOF in a background goroutine.
	// The parent may send agent.cancel at any time, or close stdin
	// (parent death / intentional shutdown). Both trigger cancellation.
	cancelCh := make(chan struct{})
	go func() {
		for {
			typ, _, err := stdin.ReadEnvelope()
			if err != nil {
				// stdin EOF: parent died or closed the pipe.
				// Treat as cancel-with-no-listener - persist and exit silently.
				slog.Info("child: stdin closed, treating as cancel", "err", err)
				close(cancelCh)
				return
			}
			if typ == "agent.cancel" {
				slog.Info("child: received agent.cancel")
				close(cancelCh)
				return
			}
		}
	}()

	// Step 7: Stream events until completion, cancellation, or error.
	chatSub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("child"))
	defer chatSub.Close()

	turns := 0
	var finalText strings.Builder
	var lastError string

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - exit without writing result.
			return fmt.Errorf("context cancelled: %w", ctx.Err())

		case <-cancelCh:
			// Parent cancelled or stdin EOF - cancel the active turn.
			_ = coordinator.Send(tauchat.CancelChatRequestCommand{
				SessionID: assign.SessionID,
			})
			// The Cancelled event handler below will persist and write result.
			continue

		case event, ok := <-chatSub.Events():
			if !ok {
				return errors.New("event stream closed unexpectedly")
			}
			switch e := event.(type) {
			case tauchat.ChatResponseDeltaEvent:
				if e.SessionID == assign.SessionID {
					finalText.WriteString(e.Delta)
				}

			case tauchat.ChatToolExecutionStartedEvent:
				if e.SessionID == assign.SessionID {
					turns++
				}

			case tauchat.ChatResponseCompletedEvent:
				if e.State.SessionID != assign.SessionID {
					continue
				}
				if e.State.LastError != "" {
					lastError = e.State.LastError
				}
				status := childStatus(lastError)
				if e.BudgetExhausted {
					status = string(tauchat.ChildAgentBudgetExhausted)
				}
				if e.TimedOut {
					status = string(tauchat.ChildAgentTimedOut)
				}
				// Always persist before sending result.
				if sessionMgr != nil {
					_ = sessionMgr.Save(ctx, e.State, 0)
				}
				if err := stdout.WriteEnvelope("agent.result", bridge.AgentResult{
					TaskID:    assign.TaskID,
					Status:    status,
					FinalText: finalText.String(),
					SessionID: assign.SessionID,
					Usage: bridge.AgentResultUsage{
						Turns:        turns,
						InputTokens:  e.State.LastUsage.PromptTokens,
						OutputTokens: e.State.LastUsage.CompletionTokens,
					},
					Error:   lastError,
					Partial: e.Partial,
				}); err != nil {
					return fmt.Errorf("write agent.result: %w", err)
				}
				return nil

			case tauchat.ChatRuntimeErrorEvent:
				if e.SessionID != assign.SessionID {
					continue
				}
				_ = stdout.WriteEnvelope("agent.result", bridge.AgentResult{
					TaskID:    assign.TaskID,
					Status:    string(tauchat.ChildAgentFailed),
					FinalText: finalText.String(),
					SessionID: assign.SessionID,
					Error:     e.Message,
					Partial:   true,
				})
				return fmt.Errorf("runtime error: %s", e.Message)

			case tauchat.ChatResponseCancelledEvent:
				if e.State.SessionID != assign.SessionID {
					continue
				}
				// Always persist before sending result.
				if sessionMgr != nil {
					_ = sessionMgr.Save(ctx, e.State, 0)
				}
				_ = stdout.WriteEnvelope("agent.result", bridge.AgentResult{
					TaskID:    assign.TaskID,
					Status:    string(tauchat.ChildAgentCancelled),
					FinalText: finalText.String(),
					SessionID: assign.SessionID,
					Usage: bridge.AgentResultUsage{
						Turns:        turns,
						InputTokens:  e.State.LastUsage.PromptTokens,
						OutputTokens: e.State.LastUsage.CompletionTokens,
					},
					Partial: true,
				})
				return nil
			}
		}
	}
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
