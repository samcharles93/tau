// Package agent implements the agent coordinator — the single runtime that
// mediates between the TUI and the LLM. It owns the agentic turn loop:
// stream a completion, detect tool_calls, execute tools in parallel,
// feed results back, and loop until the model produces a final text response.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

const (
	commandBufferSize   = 16
	toolSummaryMaxBytes = 600

	// toolCallSummaryProperty is injected into every tool's JSON schema so
	// the model can optionally supply a one-line description of what a
	// given call is doing, surfaced live in the UI status bar.
	toolCallSummaryProperty = "summary"
	toolCallSummaryMaxChars = 80
)

// TokenSource resolves a bearer token for the configured provider.
type TokenSource func(ctx context.Context, provider tauconfig.ProviderConfig) (string, error)

// Streamer is the interface for making streaming LLM calls.
// The coordinator calls StreamChatCompletionFull once per turn-loop iteration.
type Streamer interface {
	StreamChatCompletionFull(
		ctx context.Context,
		session chat.ChatSessionState,
		bearerToken string,
		extraHeaders map[string]string,
		cb chat.StreamCallbacks,
	) (chat.CompletionResult, error)
}

// Coordinator is the agent runtime that replaces chat.Runtime.
// It receives commands from the TUI, runs the agentic turn loop,
// and publishes events via the event bus.
type Coordinator struct {
	ctx               context.Context
	cancel            context.CancelFunc
	logger            *slog.Logger
	bus               *eventbus.Bus
	client            *eventbus.Client
	chatPub           *eventbus.Publisher[chat.ChatEvent]
	pluginPub         *eventbus.Publisher[chat.PluginLifecycleEvent]
	metricsPub        *eventbus.Publisher[chat.MetricEvent]
	tokenSource       TokenSource
	streamer          Streamer
	registry          *tools.Registry
	parallelToolCalls bool
	interactiveUI     bool
	uiBridge          tools.UIBridge
	extensionReloader chat.ExtensionReloader
	sessionManager    *sessions.Manager
	noPersist         bool
	autoExportJSONL   bool
	onClose           func()
	onPluginEvent     func(event string, sessionID string, payload *api.EventPayload) *api.EventResponse
	startupEvents     []chat.ChatEvent
	startupEventsOnce sync.Once
	commands          chan chat.ChatCommand
	modelLookup       ModelLookup
	projectDir        string
	skillTracker      *skills.Tracker
	skillsMgr         *skills.Manager
	commandRegistry   *commandreg.Registry
	lastSkillsConfig  skills.DiscoveryConfig
	effectiveTools    map[string]bool // immutable ceiling (set at construction); nil = unrestricted
	allowedTools      map[string]bool // active filter (intersected with effectiveTools); nil = no mode restriction
	autoCompact       tauconfig.AutoCompactConfig

	// Budget/limit enforcement (per-task, used by child processes).
	maxTurns         int
	timeout          time.Duration
	maxTokens        int
	deadline         time.Time
	startedAt        time.Time
	turnCount        int
	cumulativeTokens int

	mu       sync.Mutex
	sessions map[string]*coordinatorSession
	shutdown map[string]struct{}

	closeOnce sync.Once
	done      chan struct{}
	loopDone  chan struct{}
	turnWG    sync.WaitGroup
	promptMu  sync.Mutex
	prompts   map[string]chan interactivePromptResponse
	promptSeq atomic.Uint64
}

type coordinatorSession struct {
	state           *chat.ChatSessionState
	cancel          context.CancelFunc
	steeringMu      sync.Mutex
	pendingSteering []string
	turnStartedAt   time.Time

	// bashCancel cancels the session's in-flight bash-mode ("!") command, if
	// any. Independent of cancel (the turn-cancellation func above) since a
	// bash command runs outside the LLM turn loop and can be in flight
	// concurrently with a turn.
	bashCancel context.CancelFunc

	// Tool-call loop breaker state — see checkToolLoop. Guarded by its own
	// mutex (not c.mu) since it's touched from executeTool, which may run
	// concurrently across goroutines when parallelToolCalls is true.
	toolLoopMu      sync.Mutex
	lastToolKey     string // normalized name+args of the last tool call
	lastToolStreak  int    // consecutive calls matching lastToolKey
	lastToolBlocked int    // consecutive unjustified blocks for the current streak
}

// CoordinatorConfig holds the dependencies for creating a Coordinator.
// ModelLookup resolves a model ID to its full ChatModelRef (with Config,
// context window, and pricing). Returns nil if the model is unknown.
type ModelLookup func(modelID string) *chat.ChatModelRef

type CoordinatorConfig struct {
	Bus         *eventbus.Bus
	TokenSource TokenSource
	Streamer    Streamer
	Registry    *tools.Registry

	// Logger is the root logger for the coordinator. A child logger with
	// component=coordinator is created automatically. When nil, the
	// package-level slog.Default() is used.
	Logger *slog.Logger

	ParallelToolCalls *bool // nil → default (true)
	InteractiveUI     bool
	ExtensionReloader chat.ExtensionReloader
	SessionManager    *sessions.Manager
	// NoPersist skips writing session state to the store on close/shutdown
	// (--ephemeral). Zero value (false) preserves today's persist-by-default
	// behaviour for every existing caller.
	NoPersist       bool
	AutoExportJSONL bool
	OnClose         func()
	StartupEvents   []chat.ChatEvent

	// ProjectDir is the project root directory containing .tau.yaml.
	// When set, provider/model changes from the UI are persisted to the local
	// config file so they survive restarts. Empty means no config file writes.
	ProjectDir string

	// SkillTracker records skills activated in the current session.
	SkillTracker *skills.Tracker

	// SkillsManager is the catalog used to resolve skill names for user-invoked
	// skill activation (RunSkillCommand).
	SkillsManager *skills.Manager

	// CommandRegistry is the command registry used to re-merge skills on
	// hot-reload (ReloadSkillsCommand). Nil means no registry updates.
	CommandRegistry *commandreg.Registry

	// SkillsDiscoveryConfig is the last DiscoveryConfig used by the skills
	// manager, stored so hot-reload can re-use the same sources/paths.
	SkillsDiscoveryConfig skills.DiscoveryConfig

	// OnPluginEvent dispatches lifecycle events to the plugin manager.
	// The coordinator fires this at turn boundaries, tool execution boundaries,
	// and LLM request boundaries. sessionID is the explicit session identity.
	// Returns merged EventResponse, or nil if no plugins.
	OnPluginEvent func(event string, sessionID string, payload *api.EventPayload) *api.EventResponse

	// AllowedTools is the initial tool allowlist for this coordinator.
	// When non-empty, the LLM can only see tools in this list (plus the
	// "skill" tool, which is always added for mode switching). An empty
	// or nil slice means no restriction — the full registry is available.
	// This is the process-wide base; individual modes may further narrow
	// the set via SetAllowedTools, which always intersects with this base.
	AllowedTools []string

	// ModelLookup resolves a model ID to its full ChatModelRef (with Config,
	// context window, pricing). If set, the coordinator calls this whenever
	// a model patch arrives to enrich the bare {id: "..."} with the full
	// metadata so snapshots carry correct context_window and cost.
	ModelLookup ModelLookup

	// MetricsConfig controls observability export. Session tracking is
	// always on; Dir enables file export when non-empty.
	MetricsConfig tauconfig.MetricsConfig

	// AutoCompact controls automatic conversation-history compaction before
	// LLM requests when the current request approaches the model context
	// window.
	AutoCompact tauconfig.AutoCompactConfig

	// MaxTurns is a structural cap on agentic-loop iterations per assigned
	// task (from spec or config). Zero means no cap.
	MaxTurns int
	// Timeout is the default wall-clock limit per task (from spec or
	// config). Zero means no timeout.
	Timeout time.Duration
	// MaxTokens is a per-task token budget from the spawn call. Zero
	// means no budget.
	MaxTokens int
	// Deadline is a per-task wall-clock deadline from the spawn call.
	// Zero means no deadline.
	Deadline time.Time
}

// NewCoordinator creates and starts the agent coordinator.
func NewCoordinator(ctx context.Context, cfg CoordinatorConfig) (*Coordinator, error) {
	if cfg.TokenSource == nil {
		return nil, errors.New("agent token source is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("agent tool registry is required")
	}
	if cfg.Streamer == nil {
		return nil, errors.New("agent streamer is required")
	}
	if cfg.Bus == nil {
		return nil, errors.New("agent event bus is required")
	}

	ctx, cancel := context.WithCancel(ctx)
	parallel := true
	if cfg.ParallelToolCalls != nil {
		parallel = *cfg.ParallelToolCalls
	}

	client := cfg.Bus.Client("coordinator")
	chatPub := eventbus.Publish[chat.ChatEvent](client)
	pluginPub := eventbus.Publish[chat.PluginLifecycleEvent](client)
	metricsPub := eventbus.Publish[chat.MetricEvent](client)

	logger := slog.Default()
	if cfg.Logger != nil {
		logger = cfg.Logger
	}
	logger = logger.With("component", "coordinator")

	// Initialise the tool allowlist from config. Empty/nil means unrestricted.
	// The "skill" tool is always added to non-empty filters so mode switching
	// remains available even in restricted tool sets.
	var initEffectiveTools map[string]bool
	if len(cfg.AllowedTools) > 0 {
		initEffectiveTools = make(map[string]bool, len(cfg.AllowedTools)+1)
		for _, name := range cfg.AllowedTools {
			initEffectiveTools[strings.TrimSpace(name)] = true
		}
		initEffectiveTools["skill"] = true
	}

	c := &Coordinator{
		ctx:               ctx,
		cancel:            cancel,
		logger:            logger,
		bus:               cfg.Bus,
		client:            client,
		chatPub:           chatPub,
		pluginPub:         pluginPub,
		metricsPub:        metricsPub,
		tokenSource:       cfg.TokenSource,
		streamer:          cfg.Streamer,
		registry:          cfg.Registry,
		parallelToolCalls: parallel,
		interactiveUI:     cfg.InteractiveUI,
		extensionReloader: cfg.ExtensionReloader,
		sessionManager:    cfg.SessionManager,
		noPersist:         cfg.NoPersist,
		autoExportJSONL:   cfg.AutoExportJSONL,
		onClose:           cfg.OnClose,
		onPluginEvent:     cfg.OnPluginEvent,
		startupEvents:     append([]chat.ChatEvent(nil), cfg.StartupEvents...),
		commands:          make(chan chat.ChatCommand, commandBufferSize),
		modelLookup:       cfg.ModelLookup,
		projectDir:        cfg.ProjectDir,
		skillTracker:      cfg.SkillTracker,
		skillsMgr:         cfg.SkillsManager,
		commandRegistry:   cfg.CommandRegistry,
		lastSkillsConfig:  cfg.SkillsDiscoveryConfig,
		autoCompact:       cfg.AutoCompact,
		maxTurns:          cfg.MaxTurns,
		timeout:           cfg.Timeout,
		maxTokens:         cfg.MaxTokens,
		deadline:          cfg.Deadline,
		startedAt:         time.Now(),
		effectiveTools:    initEffectiveTools,
		allowedTools:      nil,
		sessions:          make(map[string]*coordinatorSession),
		shutdown:          make(map[string]struct{}),
		prompts:           make(map[string]chan interactivePromptResponse),
		done:              make(chan struct{}),
		loopDone:          make(chan struct{}),
	}

	if cfg.InteractiveUI {
		c.uiBridge = &coordinatorUIBridge{coordinator: c}
	} else {
		c.uiBridge = tools.NonInteractiveBridge{}
	}

	go func() {
		defer close(c.loopDone)
		c.loop()
	}()

	return c, nil
}

// SubscribeEvents returns a typed subscriber for coordinator events.
// The subscriber's Events channel carries [chat.ChatEvent] values in
// publication order. Startup events (configured via
// CoordinatorConfig.StartupEvents) are delivered when the first
// subscriber connects.
func (c *Coordinator) SubscribeEvents() (*eventbus.Subscriber[chat.ChatEvent], error) {
	sub := eventbus.Subscribe[chat.ChatEvent](c.client)
	c.startupEventsOnce.Do(func() {
		for _, event := range c.startupEvents {
			c.chatPub.Publish(event)
		}
	})
	return sub, nil
}

// Send submits a command to the coordinator.
func (c *Coordinator) Send(cmd chat.ChatCommand) error {
	if cmd == nil {
		return errors.New("command is required")
	}
	select {
	case <-c.done:
		return errors.New("coordinator is closed")
	case <-c.ctx.Done():
		return errors.New("coordinator is closed")
	case c.commands <- cmd:
		return nil
	}
}

// Close shuts down the coordinator gracefully.
func (c *Coordinator) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		c.cancelInteractivePrompts()
		<-c.loopDone
		if c.onClose != nil {
			c.onClose()
		}
		c.client.Close()
		close(c.done)
	})
}

func (c *Coordinator) loop() {
	for {
		select {
		case <-c.ctx.Done():
			c.cancelAllSessions()
			c.turnWG.Wait()
			return
		case cmd := <-c.commands:
			c.handleCommand(cmd)
		}
	}
}

func (c *Coordinator) handleCommand(cmd chat.ChatCommand) {
	c.loggerOrDefault().Debug("chat command received", chatCommandLogAttrs(cmd)...)

	switch command := cmd.(type) {
	case chat.StartChatSessionCommand:
		c.handleStart(command)
	case chat.SubmitChatPromptCommand:
		c.handleSubmit(command)
	case chat.SteerChatPromptCommand:
		c.handleSteer(command)
	case chat.UpdateChatSessionCommand:
		c.handleUpdate(command)
	case chat.CancelChatRequestCommand:
		c.handleCancel(command)
	case chat.ResetChatSessionCommand:
		c.handleReset(command)
	case chat.CloseChatSessionCommand:
		c.handleClose(command)
	case chat.ReloadExtensionsCommand:
		c.handleReloadExtensions(command)
	case chat.RunExtensionCommandCommand:
		c.handleRunExtensionCommand(command)
	case chat.RespondInteractivePromptCommand:
		c.handleInteractivePromptResponse(command)
	case chat.ListSessionsCommand:
		c.handleListSessions(command)
	case chat.LoadSessionCommand:
		c.handleLoadSession(command)
	case chat.DeleteSessionCommand:
		c.handleDeleteSession(command)
	case chat.ExportSessionCommand:
		c.handleExportSession(command)
	case chat.RunSkillCommand:
		c.handleRunSkill(command)
	case chat.RunAgentCommand:
		c.handleRunAgent(command)
	case chat.RunBashCommand:
		c.handleRunBashCommand(command)
	case chat.CancelBashCommand:
		c.handleCancelBash(command)
	case chat.ReloadSkillsCommand:
		c.handleReloadSkills(command)
	case chat.ListSkillsCommand:
		c.handleListSkills(command)
	default:
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("unsupported command %T", cmd),
			Fatal:      true,
			OccurredAt: time.Now().UTC(),
		})
	}
}

func (c *Coordinator) handleStart(cmd chat.StartChatSessionCommand) {
	now := time.Now().UTC()
	state, err := chat.NewChatSessionState(cmd.SessionID, cmd.Config, now)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}

	c.mu.Lock()
	if _, exists := c.sessions[state.SessionID]; exists {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  state.SessionID,
			Message:    "session already exists",
			Fatal:      true,
			OccurredAt: now,
		})
		return
	}
	c.sessions[state.SessionID] = &coordinatorSession{state: state}
	snapshot := chat.CloneChatSessionState(state)
	c.mu.Unlock()

	c.publishPluginLifecycleEvent("session_start", snapshot.SessionID, &api.EventPayload{
		Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{
			SessionId: snapshot.SessionID,
			ModelId:   snapshot.Model.ID,
			Provider:  snapshot.Provider.Name,
		}},
	})
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategorySession,
		Name:     "session.created",
		Value:    1,
		Unit:     "count",
		Labels: map[string]string{
			"provider": snapshot.Provider.Name,
			"model":    snapshot.Model.ID,
		},
		SessionID: snapshot.SessionID,
	})
}

func (c *Coordinator) handleSubmit(cmd chat.SubmitChatPromptCommand) {
	now := normalizedTime(cmd.SubmittedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.BeginTurn(cmd.RequestID, cmd.Prompt, now); err != nil {
		c.mu.Unlock()
		if strings.Contains(err.Error(), "already in flight") {
			// Silently drop duplicate submits — the real turn is already running.
			return
		}
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	turnState := chat.CloneChatSessionState(session.state)
	turnCtx, cancel := context.WithCancel(c.ctx)
	session.cancel = cancel
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emit(chat.ChatResponseStartedEvent{
		SessionID: cmd.SessionID,
		RequestID: cmd.RequestID,
		StartedAt: now,
	})

	c.loggerWithTurn(cmd.SessionID, cmd.RequestID).Debug(
		"turn started",
		"provider", turnState.ProviderName,
		"model", turnState.Model.ID,
		"msg_count", len(turnState.Messages),
	)

	c.turnWG.Go(func() {
		c.runTurn(turnCtx, turnState)
	})
}

func (c *Coordinator) handleUpdate(cmd chat.UpdateChatSessionCommand) {
	now := time.Now().UTC()

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	// Capture old values for model/provider switch metrics.
	oldModel := session.state.Model.ID
	oldProvider := session.state.ProviderName

	// If the patch changes the model ID, try to enrich the bare {id: "..."}
	// with the full Config (context window, pricing) so snapshots carry
	// correct metadata to wire consumers.
	if cmd.Patch.Model != nil && c.modelLookup != nil {
		if full := c.modelLookup(cmd.Patch.Model.ID); full != nil {
			cmd.Patch.Model.Config = full.Config
			cmd.Patch.Model.URL = full.URL
		}
	}

	if err := session.state.ApplyPatch(cmd.Patch, now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	// handleUpdate is a config change, not a session lifecycle transition.
	// Session shutdown is dispatched by handleClose and cancelAllSessions.
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})

	// Emit model/provider switch metrics.
	if cmd.Patch.Model != nil && oldModel != snapshot.Model.ID {
		c.emitMetrics(chat.MetricEvent{
			Category:  chat.MetricCategorySession,
			Name:      "session.model.changed",
			Value:     1,
			Unit:      "count",
			Labels:    map[string]string{"from": oldModel, "to": snapshot.Model.ID},
			SessionID: snapshot.SessionID,
		})
	}
	if cmd.Patch.Provider != nil && oldProvider != snapshot.ProviderName {
		c.emitMetrics(chat.MetricEvent{
			Category:  chat.MetricCategorySession,
			Name:      "session.provider.changed",
			Value:     1,
			Unit:      "count",
			Labels:    map[string]string{"from": oldProvider, "to": snapshot.ProviderName},
			SessionID: snapshot.SessionID,
		})
	}

	// Persist provider/model changes to the local config file so they survive
	// restarts. This runs outside the mutex (file I/O is blocking).
	c.persistDefaultsOnUpdate(cmd.Patch, snapshot)
}

func (c *Coordinator) handleCancel(cmd chat.CancelChatRequestCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	// Cancel semantics:
	//   * If a non-empty request_id is supplied and it matches the active
	//     request, proceed normally.
	//   * If a non-empty request_id is supplied and it does NOT match the
	//     active request, but a different request is in flight, cancel the
	//     in-flight one. This handles reconnect/double-submit races on the
	//     web transport where the client and server request_id can briefly
	//     diverge.
	//   * If no request is in flight at all, succeed silently — the user's
	//     intent (stop waiting) is already satisfied.
	if !session.state.HasActiveRequest() {
		c.mu.Unlock()
		c.emit(chat.ChatSessionSnapshotEvent{State: chat.CloneChatSessionState(session.state)})
		return
	}
	if cmd.RequestID != "" && session.state.ActiveRequestID != cmd.RequestID {
		// Stale request_id with a different in-flight request: cancel the
		// in-flight one and report the mismatch as a non-fatal notice so the
		// client can refresh its view of the active id.
		c.emit(chat.ChatNotificationEvent{
			Message:    "cancel targeted a different request; cancelling the active one",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
	}
	if err := session.state.MarkCancelling(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	cancel := session.cancel
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	if cancel != nil {
		cancel()
	}
}

func (c *Coordinator) handleReset(cmd chat.ResetChatSessionCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.ResetConversation(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleSteer(cmd chat.SteerChatPromptCommand) {
	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}
	session.steeringMu.Lock()
	session.pendingSteering = append(session.pendingSteering, cmd.Prompt)
	session.steeringMu.Unlock()
	c.mu.Unlock()
}

func (c *Coordinator) injectSteering(sessionID string) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return
	}

	session.steeringMu.Lock()
	steers := session.pendingSteering
	session.pendingSteering = nil
	session.steeringMu.Unlock()

	if len(steers) == 0 {
		c.mu.Unlock()
		return
	}

	for _, prompt := range steers {
		session.state.Messages = append(session.state.Messages, chat.ChatMessage{
			ID:        chat.NewMessageID(),
			Role:      chat.ChatRoleUser,
			Content:   prompt,
			CreatedAt: normalizedTime(time.Time{}),
		})
	}
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	// Emit snapshot so TUI sees the injected user messages.
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleClose(cmd chat.CloseChatSessionCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	if err := session.state.Close(now); err != nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	snapshot := chat.CloneChatSessionState(session.state)
	duration := now.Sub(snapshot.CreatedAt)
	delete(c.sessions, cmd.SessionID)
	c.mu.Unlock()

	sessionID := snapshot.SessionID
	c.mu.Lock()
	if _, exists := c.shutdown[sessionID]; !exists {
		c.shutdown[sessionID] = struct{}{}
		c.mu.Unlock()
		c.publishPluginLifecycleEvent("session_shutdown", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{
				SessionId: sessionID,
				ModelId:   snapshot.Model.ID,
				Provider:  snapshot.Provider.Name,
			}},
		})
	} else {
		c.mu.Unlock()
	}
	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategorySession,
		Name:     "session.closed",
		Value:    float64(duration.Milliseconds()),
		Unit:     "ms",
		Labels: map[string]string{
			"provider":      snapshot.ProviderName,
			"model":         snapshot.Model.ID,
			"message_count": fmt.Sprintf("%d", len(snapshot.Messages)),
		},
		SessionID: snapshot.SessionID,
	})

	c.persistSession(snapshot, duration)
	// Clean up shutdown dedup entry now that the session is fully closed.
	c.mu.Lock()
	delete(c.shutdown, sessionID)
	c.mu.Unlock()
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
}

func (c *Coordinator) handleInteractivePromptResponse(cmd chat.RespondInteractivePromptCommand) {
	c.promptMu.Lock()
	ch := c.prompts[cmd.RequestID]
	if ch != nil {
		delete(c.prompts, cmd.RequestID)
	}
	c.promptMu.Unlock()
	if ch == nil {
		return
	}
	ch <- interactivePromptResponse{confirmed: cmd.Confirmed, canceled: cmd.Canceled, response: cmd.Response}
}

// UIBridge returns the coordinator's interactive UI bridge so plugin commands
// can prompt the user via the HostService Confirm/Input RPCs.
func (c *Coordinator) UIBridge() tools.UIBridge {
	return c.uiBridge
}

func (c *Coordinator) isIdle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, session := range c.sessions {
		if session.state.Status == chat.ChatSessionStreaming || session.state.Status == chat.ChatSessionCancelling {
			return false
		}
		if session.state.HasActiveRequest() {
			return false
		}
	}
	return true
}

// loggerWith returns a child logger that carries the given session_id on
// every log line. Call once per session and reuse.
func (c *Coordinator) loggerWith(sessionID string) *slog.Logger {
	return c.loggerOrDefault().With("session_id", sessionID)
}

// loggerWithTurn returns a child logger that carries both session_id and
// request_id, suitable for use within a single turn.
func (c *Coordinator) loggerWithTurn(sessionID, requestID string) *slog.Logger {
	return c.loggerOrDefault().With("session_id", sessionID, "request_id", requestID)
}

func (c *Coordinator) loggerOrDefault() *slog.Logger {
	if c != nil && c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

func normalizedTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

func shortTypeName(v any) string {
	name := fmt.Sprintf("%T", v)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func chatCommandLogAttrs(cmd chat.ChatCommand) []any {
	attrs := []any{"command_type", shortTypeName(cmd)}
	switch c := cmd.(type) {
	case chat.StartChatSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID, "provider", c.Config.Provider.Name, "model", c.Config.Model.ID)
	case chat.SubmitChatPromptCommand:
		attrs = append(attrs, "session_id", c.SessionID, "request_id", c.RequestID, "prompt_bytes", len(c.Prompt))
	case chat.SteerChatPromptCommand:
		attrs = append(attrs, "session_id", c.SessionID, "request_id", c.RequestID, "prompt_bytes", len(c.Prompt))
	case chat.UpdateChatSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID)
		if c.Patch.Model != nil {
			attrs = append(attrs, "model", c.Patch.Model.ID)
		}
		if c.Patch.Provider != nil {
			attrs = append(attrs, "provider", *c.Patch.Provider)
		}
	case chat.CancelChatRequestCommand:
		attrs = append(attrs, "session_id", c.SessionID, "request_id", c.RequestID)
	case chat.ResetChatSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID)
	case chat.CloseChatSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID)
	case chat.ReloadExtensionsCommand:
		attrs = append(attrs, "requested_at", c.RequestedAt)
	case chat.RunExtensionCommandCommand:
		attrs = append(attrs, "name", c.Name, "args_bytes", len(c.Args))
	case chat.RespondInteractivePromptCommand:
		attrs = append(attrs, "prompt_request_id", c.RequestID, "confirmed", c.Confirmed, "canceled", c.Canceled, "response_bytes", len(c.Response))
	case chat.LoadSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID)
	case chat.DeleteSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID)
	case chat.ExportSessionCommand:
		attrs = append(attrs, "session_id", c.SessionID, "format", c.Format, "path_set", c.Output != "")
	case chat.RunSkillCommand:
		attrs = append(attrs, "session_id", c.SessionID, "skill", c.SkillName, "args_bytes", len(c.Args))
	case chat.RunAgentCommand:
		attrs = append(attrs, "session_id", c.SessionID, "agent", c.Name)
	case chat.RunBashCommand:
		attrs = append(attrs, "session_id", c.SessionID, "call_id", c.CallID, "command_bytes", len(c.Command), "exclude", c.Exclude)
	case chat.CancelBashCommand:
		attrs = append(attrs, "session_id", c.SessionID)
	}
	return attrs
}

func chatEventLogAttrs(event chat.ChatEvent) []any {
	attrs := []any{"event_type", shortTypeName(event)}
	switch e := event.(type) {
	case chat.ChatSessionSnapshotEvent:
		attrs = append(
			attrs,
			"session_id", e.State.SessionID,
			"request_id", e.State.ActiveRequestID,
			"status", e.State.Status,
			"message_count", len(e.State.Messages),
			"pending_assistant_bytes", len(e.State.PendingAssistant),
			"tool_count", len(e.State.Tools),
		)
	case chat.ChatResponseStartedEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID)
	case chat.ChatResponseDeltaEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "delta_bytes", len(e.Delta), "snapshot_bytes", len(e.Snapshot))
	case chat.ChatReasoningDeltaEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "delta_bytes", len(e.Delta), "snapshot_bytes", len(e.Snapshot))
	case chat.ChatToolCallDeltaEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "call_id", e.CallID, "index", e.Index, "tool", e.ToolName, "args_summary_bytes", len(e.ArgumentsSummary), "truncated", e.Truncated)
	case chat.ChatToolExecutionStartedEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "call_id", e.CallID, "tool", e.ToolName, "args_summary_bytes", len(e.ArgumentsSummary))
	case chat.ChatToolExecutionCompletedEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "call_id", e.CallID, "tool", e.ToolName, "status", e.Status, "is_error", e.IsError, "duration_ms", e.Duration.Milliseconds(), "result_summary_bytes", len(e.ResultSummary), "truncated", e.Truncated)
	case chat.ChatToolOutputEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "call_id", e.CallID, "chunk_bytes", len(e.Chunk))
	case chat.ChatResponseCompletedEvent:
		attrs = append(attrs, "session_id", e.State.SessionID, "request_id", e.RequestID, "finish_reason", e.FinishReason, "message_count", len(e.State.Messages), "total_tokens", e.Usage.TotalTokens)
	case chat.ChatResponseCancelledEvent:
		attrs = append(attrs, "session_id", e.State.SessionID, "request_id", e.RequestID, "message_count", len(e.State.Messages))
	case chat.ChatRuntimeErrorEvent:
		attrs = append(attrs, "session_id", e.SessionID, "request_id", e.RequestID, "fatal", e.Fatal, "message_bytes", len(e.Message))
	case chat.ChatNotificationEvent:
		attrs = append(attrs, "level", e.Level, "message_bytes", len(e.Message))
	case chat.ExtensionsReloadedEvent:
		attrs = append(attrs, "extension_count", e.Result.ExtensionCount, "diagnostic_count", len(e.Result.Diagnostics), "command_count", len(e.Result.Commands))
	case chat.ExtensionCommandsChangedEvent:
		attrs = append(attrs, "command_count", len(e.Commands))
	case chat.CommandsChangedEvent:
		attrs = append(attrs, "command_count", len(e.Commands))
	case chat.SkillsChangedEvent:
		attrs = append(attrs, "skill_count", len(e.Skills))
	case chat.ExtensionCommandResultEvent:
		attrs = append(attrs, "name", e.Name, "output_bytes", len(e.Output), "has_view", e.View != nil)
	case chat.ExtensionViewRenderedEvent:
		attrs = append(attrs, "plugin", e.PluginName, "view_id", e.ViewID, "widget_count", len(e.View.Widgets))
	case chat.ExtensionViewClosedEvent:
		attrs = append(attrs, "plugin", e.PluginName, "view_id", e.ViewID)
	case chat.InteractivePromptRequestedEvent:
		attrs = append(attrs, "prompt_request_id", e.RequestID, "kind", e.Kind, "title_bytes", len(e.Title), "message_bytes", len(e.Message))
	case chat.SessionsListedEvent:
		attrs = append(attrs, "session_count", len(e.Sessions), "has_next_cursor", e.NextCursor != "")
	case chat.SessionLoadedEvent:
		attrs = append(attrs, "session_id", e.State.SessionID, "message_count", len(e.State.Messages))
	case chat.SessionDeletedEvent:
		attrs = append(attrs, "session_id", e.SessionID)
	case chat.SessionExportedEvent:
		attrs = append(attrs, "session_id", e.SessionID, "format", e.Format, "path_set", e.Path != "")
	}
	return attrs
}

func pluginPayloadKind(payload *api.EventPayload) string {
	if payload == nil {
		return ""
	}
	switch payload.GetKind().(type) {
	case *api.EventPayload_Session:
		return "session"
	case *api.EventPayload_Context:
		return "context"
	case *api.EventPayload_Turn:
		return "turn"
	case *api.EventPayload_BeforeLlmCall:
		return "before_llm_call"
	case *api.EventPayload_AfterLlmCall:
		return "after_llm_call"
	case *api.EventPayload_BeforeToolExec:
		return "before_tool_exec"
	case *api.EventPayload_AfterToolExec:
		return "after_tool_exec"
	case *api.EventPayload_MessageDelta:
		return "message_delta"
	case *api.EventPayload_Compaction:
		return "compaction"
	default:
		return shortTypeName(payload.GetKind())
	}
}

func (c *Coordinator) emit(event chat.ChatEvent) {
	c.loggerOrDefault().Debug("chat event emitted", chatEventLogAttrs(event)...)
	// Runtime errors get a full-text log line at Error level, independent of
	// the Debug summary above (which only carries message_bytes) and of
	// whatever the UI truncates for display. Without this the untruncated
	// error text exists nowhere once the TUI clips it for scrollback.
	if e, ok := event.(chat.ChatRuntimeErrorEvent); ok {
		c.loggerOrDefault().Error("chat runtime error", "session_id", e.SessionID, "request_id", e.RequestID, "fatal", e.Fatal, "message", e.Message)
	}
	c.chatPub.Publish(event)
}
