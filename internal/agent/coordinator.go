// Package agent implements the agent coordinator — the single runtime that
// mediates between the TUI and the LLM. It owns the agentic turn loop:
// stream a completion, detect tool_calls, execute tools in parallel,
// feed results back, and loop until the model produces a final text response.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
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
	allowedTools      map[string]bool
	autoCompact       tauconfig.AutoCompactConfig

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

// persistDefaultsOnUpdate writes default_provider and/or default_model to the
// local .tau.yaml when the user changed provider or model through the UI.
func (c *Coordinator) persistDefaultsOnUpdate(patch chat.ChatSessionPatch, snapshot chat.ChatSessionState) {
	if c.projectDir == "" {
		return
	}
	provider := ""
	model := ""
	if patch.Provider != nil {
		provider = snapshot.ProviderName
	}
	if patch.Model != nil {
		model = snapshot.Model.ID
	}
	if provider == "" && model == "" {
		return
	}
	if err := tauconfig.SaveDefaultProviderAndModel(c.projectDir, provider, model); err != nil {
		c.loggerWith(snapshot.SessionID).Error(
			"saving default provider/model to local config",
			"err", err,
		)
	}
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

// handleRunSkill activates a skill by name in response to a user-invoked
// /skill:<name> command. It looks the skill up in the catalog, records the
// activation in the tracker, applies any AllowedTools restriction, injects
// the skill's instructions into the session system prompt so the model has
// them for subsequent turns, and emits Skill-tool-style started/completed
// events so the TUI renders the lilac "loaded" box.
func (c *Coordinator) handleRunSkill(cmd chat.RunSkillCommand) {
	now := normalizedTime(cmd.RequestedAt)

	if c.skillsMgr == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "skill manager unavailable",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	name := strings.TrimSpace(cmd.SkillName)
	snapshot := c.skillsMgr.Snapshot()
	matched := findSkillInSnapshot(snapshot.ActiveSkills, name)
	if matched == nil {
		matched = findSkillInSnapshot(snapshot.AllSkills, name)
	}
	if matched == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "skill not found: " + name,
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	if c.skillTracker != nil {
		c.skillTracker.Activate(matched)
	}
	c.emitMetrics(chat.MetricEvent{
		Category:  chat.MetricCategorySkill,
		Name:      "skill.activated",
		Value:     1,
		Unit:      "count",
		Labels:    map[string]string{"skill_name": matched.Name},
		SessionID: cmd.SessionID,
	})
	if matched.AllowedTools != "" {
		c.SetAllowedTools(tools.ParseAllowedTools(matched.AllowedTools))
	} else {
		c.SetAllowedTools(nil)
	}

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if ok && session != nil && session.state != nil {
		session.state.SystemPrompt += "\n\n<activated_skill name=\"" + matched.Name + "\">\n" +
			matched.Instructions + "\n</activated_skill>"
		snap := chat.CloneChatSessionState(session.state)
		c.mu.Unlock()
		c.emit(chat.ChatSessionSnapshotEvent{State: snap})
	} else {
		c.mu.Unlock()
	}

	// Emit the Skill-tool box lifecycle so the TUI renders the lilac "loaded"
	// feedback, mirroring the agent-invoked Skill tool path.
	callID := "skill_" + matched.Name
	startedAt := time.Now().UTC()
	c.emit(chat.ChatToolExecutionStartedEvent{
		SessionID:        cmd.SessionID,
		RequestID:        "",
		CallID:           callID,
		ToolName:         "skill",
		ArgumentsSummary: `{"name":"` + matched.Name + `"}`,
		StartedAt:        startedAt,
	})
	completedAt := time.Now().UTC()
	c.emit(chat.ChatToolExecutionCompletedEvent{
		SessionID:     cmd.SessionID,
		RequestID:     "",
		CallID:        callID,
		ToolName:      "skill",
		Status:        "success",
		Duration:      completedAt.Sub(startedAt),
		ResultSummary: "loaded",
		IsError:       false,
		CompletedAt:   completedAt,
	})
}

// handleRunAgent activates an agent command (e.g. /plan, /research, or a
// filesystem-discovered /project:foo) in response to its matching slash
// command. It resolves the agent by name, renders its prompt template,
// replaces the session's system prompt with it, and applies its tool
// restriction (if any) via the same AllowedTools mechanism skills use.
// Unlike handleRunSkill, this replaces rather than appends to the system
// prompt: these are distinct operating modes, not stacked knowledge.
//
// Filesystem-discovered definitions (agentspec.Resolve returning a
// Definition with SourcePath set — i.e. not one of the embedded built-ins)
// are untrusted relative to built-ins: they may have been authored by the
// model itself rather than reviewed and shipped in the binary. Their
// requested Tools are clamped to the session's current allowlist rather
// than applied as-is, so activating one can never grant a wider tool set
// than the session already had — see clampToolsToCurrentAllowlist.
func (c *Coordinator) handleRunAgent(cmd chat.RunAgentCommand) {
	now := normalizedTime(cmd.RequestedAt)

	name := strings.TrimSpace(cmd.Name)
	def, ok := agentspec.Resolve(name, c.projectDir)
	if !ok {
		c.emit(chat.ChatNotificationEvent{
			Message:    "agent not found: " + name,
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	rendered := RenderAgentPrompt(def, c.projectDir)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok || session.state == nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	session.state.SystemPrompt = rendered
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})

	discovered := def.SourcePath != ""
	switch {
	case len(def.Tools) > 0 && discovered:
		clamped := c.clampToolsToCurrentAllowlist(def.Tools)
		if len(clamped) < len(def.Tools) {
			c.emit(chat.ChatNotificationEvent{
				Message:    fmt.Sprintf("agent %q requested tools outside the session's current allowlist; clamped to what was already allowed", def.Name),
				Level:      chat.ChatNotificationWarn,
				OccurredAt: now,
			})
		}
		if len(clamped) > 0 {
			c.SetAllowedTools(clamped)
		}
		// Else every requested tool was clamped away — do NOT call
		// SetAllowedTools(clamped) with an empty slice: SetAllowedTools
		// treats an empty list as "no restriction" (its nil-allowedTools
		// sentinel), which would grant everything, the opposite of this
		// guard's purpose. Leave the session's current allowlist as-is,
		// same treatment as the "declared no tools at all" case below.
	case len(def.Tools) > 0:
		c.SetAllowedTools(def.Tools)
	case discovered:
		// A discovered definition with no tools list at all declares no
		// restriction — but for an untrusted definition, treating that as
		// "grant every tool" would be the single largest possible
		// escalation. Leave the session's current allowlist untouched
		// instead of clearing it.
	default:
		c.SetAllowedTools(nil)
	}

	c.emit(chat.ChatNotificationEvent{
		Message:    "agent activated: " + def.Name,
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
}

// clampToolsToCurrentAllowlist intersects requested with the coordinator's
// current tool allowlist. When the session is currently unrestricted
// (allowedTools nil/empty), there is nothing to escalate past, so requested
// passes through unchanged.
func (c *Coordinator) clampToolsToCurrentAllowlist(requested []string) []string {
	c.mu.Lock()
	current := c.allowedTools
	c.mu.Unlock()

	if len(current) == 0 {
		return requested
	}

	clamped := make([]string, 0, len(requested))
	for _, name := range requested {
		if current[name] {
			clamped = append(clamped, name)
		}
	}
	return clamped
}

// handleRunBashCommand runs a "!" (or "!!") bash-mode command entered
// directly at the chat input. It executes the same registered "shell" tool
// the LLM itself uses, outside the normal turn loop, and emits the same
// started/output/completed event trio a real LLM-invoked tool call would —
// so it renders identically in the TUI — before appending the result to
// session history (unless Exclude, the "!!" variant, is set).
func (c *Coordinator) handleRunBashCommand(cmd chat.RunBashCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok || session.state == nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	session.bashCancel = cancel
	c.mu.Unlock()

	// Executing the tool blocks on the subprocess for the command's whole
	// duration, so it must run off the command-dispatch goroutine — handleCommand
	// is called synchronously from the coordinator's single-threaded command
	// loop (run()), and blocking there would stall every other command
	// (including a CancelBashCommand for this very command) until it finished.
	c.turnWG.Go(func() {
		c.runBashCommand(ctx, cancel, cmd, now)
	})
}

// runBashCommand executes the shell tool for a "!" bash command and reports
// its result, off the coordinator's command-dispatch goroutine (see
// handleRunBashCommand).
func (c *Coordinator) runBashCommand(ctx context.Context, cancel context.CancelFunc, cmd chat.RunBashCommand, now time.Time) {
	defer cancel()

	c.emit(chat.ChatToolExecutionStartedEvent{
		SessionID:        cmd.SessionID,
		CallID:           cmd.CallID,
		ToolName:         "shell",
		ArgumentsSummary: summarizeForUI(cmd.Command),
		StartedAt:        now,
	})

	bridge := &loggingUIBridge{UIBridge: c.uiBridge, sessionID: cmd.SessionID, callID: cmd.CallID, c: c}
	tool, ok := c.registry.Get("shell")
	var result tools.Result
	if !ok {
		result = tools.Result{Content: "shell tool unavailable", IsError: true}
	} else {
		argsJSON, _ := json.Marshal(tools.ShellParams{Command: cmd.Command})
		var toolErr error
		result, toolErr = tool.Execute(ctx, argsJSON, bridge)
		if toolErr != nil {
			result = tools.Result{Content: toolErr.Error(), IsError: true}
		}
	}

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if ok && session.bashCancel != nil {
		session.bashCancel = nil
	}
	// Append to history before emitting the completed event, so observers
	// that treat "completed" as "fully done" never race a subsequent read of
	// session state against this append.
	if ok && !cmd.Exclude {
		content := fmt.Sprintf("Ran `%s`\n\n```\n%s\n```", cmd.Command, result.Content)
		_ = session.state.AppendStandaloneMessage(chat.ChatRoleUser, content, time.Now().UTC())
	}
	c.mu.Unlock()

	result.StartedAt = now
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(now)
	if result.ResultBytes == 0 {
		result.ResultBytes = len(result.Content)
	}
	if result.IsError && result.ErrorKind == "" {
		result.ErrorKind = toolErrorKind("shell", result.Content)
	}
	c.emitToolCompleted(cmd.SessionID, "", chat.ChatToolCall{ID: cmd.CallID, Function: chat.ChatFunctionCall{Name: "shell"}}, result)
}

// handleCancelBash stops the session's in-flight bash-mode command, if any.
func (c *Coordinator) handleCancelBash(cmd chat.CancelBashCommand) {
	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	var cancel context.CancelFunc
	if ok {
		cancel = session.bashCancel
	}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// handleReloadSkills re-discovers skills from disk and merges them into the
// command registry, enabling hot-reload without restarting tau.
func (c *Coordinator) handleReloadSkills(_ chat.ReloadSkillsCommand) {
	now := time.Now().UTC()
	if c.skillsMgr == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "skill manager unavailable",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	snapshot, err := c.skillsMgr.Refresh(c.lastSkillsConfig)
	if err != nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    fmt.Sprintf("skills reload failed: %v", err),
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	if c.commandRegistry != nil {
		c.commandRegistry.MergeSkills(snapshot.AllSkills)
	}
	c.emit(chat.ChatNotificationEvent{
		Message:    fmt.Sprintf("skills reloaded: %d active, %d total", len(snapshot.ActiveSkills), len(snapshot.AllSkills)),
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
}

// handleListSkills prints the available skill catalog as a notification.
func (c *Coordinator) handleListSkills(_ chat.ListSkillsCommand) {
	now := time.Now().UTC()
	if c.skillsMgr == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "skill manager unavailable",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	snapshot := c.skillsMgr.Snapshot()
	if len(snapshot.ActiveSkills) == 0 {
		c.emit(chat.ChatNotificationEvent{
			Message:    "no skills available",
			Level:      chat.ChatNotificationInfo,
			OccurredAt: now,
		})
		// Still publish an empty event for consistency
		c.emit(chat.SkillsChangedEvent{
			Skills: []chat.SkillInfo{},
		})
		return
	}

	// Publish as a structured event for the TUI
	var skills []chat.SkillInfo
	for _, s := range snapshot.ActiveSkills {
		if s == nil {
			continue
		}
		skills = append(skills, chat.SkillInfo{
			Name:        s.Name,
			Description: s.Description,
			Scope:       string(s.Scope),
		})
	}
	c.emit(chat.SkillsChangedEvent{
		Skills: skills,
	})

	// The notification banner is a single-line widget, not a list view —
	// dumping the full catalog into it renders as an unwrapped wall of
	// text. SkillsChangedEvent above already gives both TUIs a proper
	// list rendering, so keep this to a short heads-up.
	c.emit(chat.ChatNotificationEvent{
		Message:    fmt.Sprintf("%d skills available", len(skills)),
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
}

// findSkillInSnapshot returns the first skill whose name matches
// case-insensitively, or nil if none.
func findSkillInSnapshot(set []*skills.Skill, name string) *skills.Skill {
	lower := strings.ToLower(name)
	for _, s := range set {
		if s != nil && strings.ToLower(s.Name) == lower {
			return s
		}
	}
	return nil
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

func (c *Coordinator) handleReloadExtensions(cmd chat.ReloadExtensionsCommand) {
	now := normalizedTime(cmd.RequestedAt)
	if c.extensionReloader == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	idle := c.isIdle()
	if !idle {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is only available while idle",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	result, err := c.extensionReloader.ReloadExtensions(c.ctx, true)
	if err != nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload failed: " + err.Error(),
			Level:      chat.ChatNotificationError,
			OccurredAt: now,
		})
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Extension reload failed: " + err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	c.emit(chat.ExtensionsReloadedEvent{Result: result, OccurredAt: now})
	c.emit(chat.ExtensionCommandsChangedEvent{
		Commands:   result.Commands,
		OccurredAt: now,
	})
	c.emit(chat.ChatNotificationEvent{
		Message:    extensionReloadMessage(result),
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Message == "" {
			continue
		}
		level := chat.ChatNotificationWarn
		if diagnostic.Severity == "error" {
			level = chat.ChatNotificationError
		}
		c.emit(chat.ChatNotificationEvent{
			Message:    extensionDiagnosticMessage(diagnostic),
			Level:      level,
			OccurredAt: now,
		})
	}
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

func (c *Coordinator) handleRunExtensionCommand(cmd chat.RunExtensionCommandCommand) {
	now := normalizedTime(cmd.RequestedAt)
	if c.extensionReloader == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension commands are not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	if !c.isIdle() {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension commands are only available while idle",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	name := strings.TrimSpace(cmd.Name)
	args := strings.TrimSpace(cmd.Args)
	c.turnWG.Go(func() {
		output, view, err := c.extensionReloader.RunExtensionCommand(c.ctx, name, args, c.uiBridge)
		at := time.Now().UTC()
		status := "success"
		if err != nil {
			status = "error"
			// A single user-facing error notification.
			c.emit(chat.ChatNotificationEvent{
				Message:    "Extension command failed: " + err.Error(),
				Level:      chat.ChatNotificationError,
				OccurredAt: at,
			})
		}
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryExtension,
			Name:     "extension.command",
			Value:    1,
			Unit:     "count",
			Labels:   map[string]string{"command": name, "status": status},
		})
		if err != nil {
			return
		}
		c.emit(chat.ExtensionCommandResultEvent{Name: name, Output: output, View: view, OccurredAt: at})
		if strings.TrimSpace(output) != "" {
			c.emit(chat.ChatNotificationEvent{
				Message:    "Extension command completed: /" + name,
				Level:      chat.ChatNotificationInfo,
				OccurredAt: at,
			})
		}
	})
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

// runTurn is the agentic turn loop. It streams a completion, and if the
// model returns tool_calls, executes them in parallel, appends results
// to the conversation, and loops. Stops when the model produces a final
// text response or an error occurs.
func (c *Coordinator) runTurn(ctx context.Context, state chat.ChatSessionState) {
	sessionID := state.SessionID
	requestID := state.ActiveRequestID
	now := time.Now().UTC()
	turnStartedAt := now

	c.mu.Lock()
	if s := c.sessions[sessionID]; s != nil {
		s.turnStartedAt = turnStartedAt
	}
	c.mu.Unlock()

	c.publishPluginLifecycleEvent("turn_start", sessionID, &api.EventPayload{
		Kind: &api.EventPayload_Turn{Turn: &api.TurnPayload{Direction: "start"}},
	})

	bearerToken, err := c.tokenSource(ctx, state.Provider)
	if err != nil {
		c.loggerWithTurn(sessionID, requestID).Debug(
			"turn failed — token source error",
			"err", err,
		)
		c.failTurn(sessionID, requestID, err, now)
		return
	}

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn loop begin",
		"provider", state.ProviderName,
		"model", state.Model.ID,
	)

	// The turn loop: call LLM, if tool_calls → execute → append → repeat.
	for iteration := 0; ; iteration++ {
		if err := ctx.Err(); err != nil {
			c.cancelTurn(sessionID, requestID, time.Now().UTC())
			return
		}

		// Inject any steering messages that arrived during tool execution
		// or at the turn start.
		c.injectSteering(sessionID)
		state = c.getSessionState(sessionID)

		// Build tool definitions from registry each iteration so the
		// AllowedTools filter (set by the Skill tool) takes effect.
		state.Tools = c.buildToolDefs()
		reasoningSnapshot := ""
		toolCallSnapshots := make([]chat.ChatToolCall, 0)

		// Emit context event with the full message list so plugins can
		// observe and mutate conversation state before the LLM call.
		contextResp := c.dispatchPluginRequestResponse("context", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_Context{Context: &api.ContextPayload{
				Messages: marshalMessages(state.Messages),
			}},
		})
		if contextResp != nil {
			c.applyPluginMessageModifications(&state, contextResp)
			// Sync mutations back to the stored session so they persist
			// across turn iterations (tool-call loops).
			c.mu.Lock()
			if s, ok := c.sessions[sessionID]; ok {
				s.state.Messages = state.Messages
			}
			c.mu.Unlock()
		}

		if iteration == 0 {
			compactedState, compacted, compactErr := c.maybeAutoCompact(ctx, state, bearerToken)
			if compactErr != nil {
				c.loggerWithTurn(sessionID, requestID).Warn(
					"auto-compaction failed",
					"err", compactErr,
				)
				c.emit(chat.ChatNotificationEvent{
					Message:    "auto-compaction failed: " + compactErr.Error(),
					Level:      chat.ChatNotificationWarn,
					OccurredAt: time.Now().UTC(),
				})
			} else if compacted {
				state = compactedState
			}
		}

		pluginResp := c.dispatchPluginRequestResponse("before_llm_call", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeLlmCall{
				BeforeLlmCall: &api.BeforeLLMCallPayload{
					ModelId:    state.Model.ID,
					Headers:    map[string]string{"Authorization": "Bearer " + bearerToken},
					Messages:   marshalMessages(state.Messages),
					Parameters: marshalParameters(state.Parameters),
				},
			},
		})

		var extraHeaders map[string]string
		if pluginResp != nil && len(pluginResp.GetAddHeaders()) > 0 {
			extraHeaders = pluginResp.GetAddHeaders()
		}

		// Apply LLM-boundary modifiers for this call only.
		originalSystemPrompt := state.SystemPrompt
		originalModelID := state.Model.ID
		if pluginResp != nil {
			if pluginResp.GetInjectSystemPrompt() != "" {
				state.SystemPrompt = state.SystemPrompt + "\n" + pluginResp.GetInjectSystemPrompt()
			}
			if pluginResp.GetModifiedModelId() != "" {
				state.Model.ID = pluginResp.GetModifiedModelId()
			}
		}

		c.loggerWithTurn(sessionID, requestID).Debug(
			"llm call start",
			"iteration", iteration,
			"model", state.Model.ID,
			"provider", state.ProviderName,
			"msg_count", len(state.Messages),
			"tool_count", len(state.Tools),
		)

		result, err := c.streamer.StreamChatCompletionFull(ctx, state, bearerToken, extraHeaders, chat.StreamCallbacks{
			OnDelta: func(delta string) error {
				return c.appendDelta(sessionID, requestID, delta, time.Now().UTC())
			},
			OnReasoningDelta: func(delta string) error {
				reasoningSnapshot += delta
				c.emitReasoningDelta(sessionID, requestID, delta, reasoningSnapshot, time.Now().UTC())
				return nil
			},
			OnToolCallDelta: func(tcd chat.ChatToolCallDelta) error {
				toolCallSnapshots = mergeToolCallDelta(toolCallSnapshots, tcd)
				call := toolCallSnapshots[tcd.Index]
				callID := call.ID
				if callID == "" {
					callID = fmt.Sprintf("tool_call_%d", tcd.Index)
				}
				argsSummary, truncated := summarizeForUIWithTruncation(call.Function.Arguments)
				c.emit(chat.ChatToolCallDeltaEvent{
					SessionID:        sessionID,
					RequestID:        requestID,
					CallID:           callID,
					Index:            tcd.Index,
					ToolName:         call.Function.Name,
					ArgumentsSummary: argsSummary,
					Truncated:        truncated,
					ReceivedAt:       time.Now().UTC(),
				})
				return nil
			},
		})
		if err != nil {
			// Restore per-call overrides before error handling.
			state.SystemPrompt = originalSystemPrompt
			state.Model.ID = originalModelID
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
				c.loggerWithTurn(sessionID, requestID).Debug(
					"llm call cancelled",
					"iteration", iteration,
					"err", err,
				)
				c.cancelTurn(sessionID, requestID, time.Now().UTC())
				return
			}
			c.loggerWithTurn(sessionID, requestID).Warn(
				"llm call failed",
				"iteration", iteration,
				"err", err,
			)
			c.failTurn(sessionID, requestID, err, time.Now().UTC())
			return
		}

		// Snapshot effective model ID before restoring, so after_llm_call
		// reports the model that was actually used.
		effectiveModelID := state.Model.ID

		// Restore per-call overrides.
		state.SystemPrompt = originalSystemPrompt
		state.Model.ID = originalModelID

		c.publishPluginLifecycleEvent("after_llm_call", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterLlmCall{
				AfterLlmCall: &api.AfterLLMCallPayload{
					ModelId:      effectiveModelID,
					FinishReason: result.FinishReason,
				},
			},
		})

		c.loggerWithTurn(sessionID, requestID).Debug(
			"llm call complete",
			"iteration", iteration,
			"finish_reason", result.FinishReason,
			"input_tokens", result.Usage.PromptTokens,
			"output_tokens", result.Usage.CompletionTokens,
			"total_tokens", result.Usage.TotalTokens,
			"tool_calls", len(result.ToolCalls),
		)

		// No tool calls → final response. Complete the turn.
		if len(result.ToolCalls) == 0 {
			c.completeTurn(sessionID, requestID, result, time.Now().UTC())
			return
		}

		// Tool calls detected. Validate arguments before committing —
		// malformed JSON in tool call arguments poisons the session history
		// and causes downstream API 400 errors on subsequent turns.
		toolNames := make([]string, len(result.ToolCalls))
		for i, tc := range result.ToolCalls {
			toolNames[i] = tc.Function.Name
		}
		c.loggerWithTurn(sessionID, requestID).Debug(
			"tool calls detected",
			"iteration", iteration,
			"count", len(result.ToolCalls),
			"tools", toolNames,
		)

		sanitized := sanitizeToolCallArguments(result.ToolCalls)
		if sanitized > 0 {
			c.emitMetrics(chat.MetricEvent{
				Category: chat.MetricCategoryTool,
				Name:     "tool.arguments.malformed",
				Value:    float64(sanitized),
				Unit:     "count",
				Labels: map[string]string{
					"total_tool_calls": fmt.Sprintf("%d", len(result.ToolCalls)),
				},
				SessionID: sessionID,
			})
		}

		// Commit the assistant's response with sanitized tool calls.
		c.commitAssistantMessage(sessionID, c.getPendingContent(sessionID), result.ReasoningContent, result.ToolCalls)

		// Execute tool calls in parallel.
		toolResults, hardStopReason := c.executeToolsParallel(ctx, sessionID, requestID, result.ToolCalls)

		// Append tool result messages to the session.
		for i, tc := range result.ToolCalls {
			c.appendToolResult(sessionID, requestID, tc, toolResults[i])
		}

		// The tool-call loop breaker tripped: a call was blocked without
		// ever being justified toolLoopHardBlockLimit times in a row. End
		// the turn now rather than let it iterate forever.
		if hardStopReason != "" {
			c.failTurn(sessionID, requestID, errors.New(hardStopReason), time.Now().UTC())
			return
		}

		// Refresh state for the next iteration.
		state = c.getSessionState(sessionID)
		if state.SessionID == "" {
			return // session gone
		}

		// Clear pending assistant for the next LLM call.
		c.clearPending(sessionID)
	}
}

// toolLoopSoftThreshold is how many consecutive identical tool calls (same
// name+arguments, ignoring the repeat_justification escape hatch below) are
// allowed before the coordinator starts requiring the model to explicitly
// justify continuing.
const toolLoopSoftThreshold = 3

// toolLoopHardBlockLimit ends the turn outright once a call has been
// blocked this many times in a row without ever being justified — a
// backstop for a model that never engages with the block message at all
// (e.g. a model stuck in decoding-level token repetition, which by
// definition can't spontaneously produce a novel justification string).
// There is deliberately no cap on justified repeats: a model that keeps
// affirming a genuinely repeated action (re-running the same test, polling
// for a state change) can continue indefinitely.
const toolLoopHardBlockLimit = 3

// toolLoopVerdict is what checkToolLoop decided for one tool call.
type toolLoopVerdict struct {
	blocked   bool // true: don't execute the real tool; return message as a synthetic error result instead
	hardStop  bool // true: end the whole turn, not just this call
	justified bool // true: at/past the threshold, but let through by an explicit repeat_justification
	streak    int  // consecutive identical calls so far, for logging/metrics
	blockedN  int  // consecutive unjustified blocks so far, for logging/metrics
	message   string
}

// parseToolCallKey unmarshals the tool call arguments once and returns both
// the normalized comparison key (name+args, excluding repeat_justification)
// and any non-empty repeat_justification string.
func parseToolCallKey(name, argsJSON string) (key string, justification string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return name + "\x00" + argsJSON, ""
	}
	s, _ := m["repeat_justification"].(string)
	justification = strings.TrimSpace(s)
	delete(m, "repeat_justification")
	normalized, err := json.Marshal(m)
	if err != nil {
		return name + "\x00" + argsJSON, justification
	}
	return name + "\x00" + string(normalized), justification
}

// checkToolLoop tracks consecutive identical tool calls (same name+args,
// ignoring repeat_justification) per session, to break a decoding-level
// repetition loop — a real incident had a model call grep with byte-
// identical arguments 103 times in a row, each getting a valid, non-empty,
// identical result, without ever producing a final answer. Past
// toolLoopSoftThreshold, a call must carry a non-empty top-level
// "repeat_justification" argument to proceed; toolLoopHardBlockLimit
// consecutive unjustified blocks for the same call ends the turn outright.
func (c *Coordinator) checkToolLoop(sessionID string, tc chat.ChatToolCall) toolLoopVerdict {
	c.mu.Lock()
	sess := c.sessions[sessionID]
	c.mu.Unlock()
	if sess == nil {
		return toolLoopVerdict{}
	}

	key, justification := parseToolCallKey(tc.Function.Name, tc.Function.Arguments)

	sess.toolLoopMu.Lock()
	defer sess.toolLoopMu.Unlock()

	if key != sess.lastToolKey {
		sess.lastToolKey = key
		sess.lastToolStreak = 1
		sess.lastToolBlocked = 0
		return toolLoopVerdict{}
	}
	sess.lastToolStreak++

	if sess.lastToolStreak <= toolLoopSoftThreshold {
		return toolLoopVerdict{}
	}
	if justification != "" {
		sess.lastToolBlocked = 0
		return toolLoopVerdict{justified: true, streak: sess.lastToolStreak}
	}

	sess.lastToolBlocked++
	if sess.lastToolBlocked >= toolLoopHardBlockLimit {
		return toolLoopVerdict{
			hardStop: true,
			streak:   sess.lastToolStreak,
			blockedN: sess.lastToolBlocked,
			message: fmt.Sprintf(
				"tool %q was called %d times in a row with identical arguments and blocked %d times without ever being justified — stopping the turn to avoid a runaway loop",
				tc.Function.Name, sess.lastToolStreak, sess.lastToolBlocked,
			),
		}
	}
	return toolLoopVerdict{
		blocked:  true,
		streak:   sess.lastToolStreak,
		blockedN: sess.lastToolBlocked,
		message: fmt.Sprintf(
			"This exact %s call has now been made %d times in a row with identical arguments. If repeating it is genuinely necessary (e.g. re-running the same test, polling for a state change), call it again with an added top-level argument %s explaining why — otherwise, try a different approach.",
			tc.Function.Name, sess.lastToolStreak, `"repeat_justification": "<short reason>"`,
		),
	}
}

// executeToolsParallel runs tool calls and returns results in input order.
// When parallelToolCalls is true, calls run concurrently; otherwise sequentially.
func (c *Coordinator) executeToolsParallel(ctx context.Context, sessionID, requestID string, calls []chat.ChatToolCall) (results []tools.Result, hardStopReason string) {
	results = make([]tools.Result, len(calls))

	toolNames := make([]string, len(calls))
	for i, tc := range calls {
		toolNames[i] = tc.Function.Name
	}
	c.loggerWithTurn(sessionID, requestID).Debug(
		"executing tools",
		"count", len(calls),
		"tools", toolNames,
		"parallel", c.parallelToolCalls,
	)

	var hardStopMu sync.Mutex

	executeTool := func(i int, tc chat.ChatToolCall) {
		startedAt := time.Now().UTC()
		effectiveArgs := tc.Function.Arguments

		loopVerdict := c.checkToolLoop(sessionID, tc)
		switch {
		case loopVerdict.hardStop:
			hardStopMu.Lock()
			if hardStopReason == "" {
				hardStopReason = loopVerdict.message
			}
			hardStopMu.Unlock()
			c.loggerWithTurn(sessionID, requestID).Warn(
				"tool loop breaker: hard stop",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
				"blocked", loopVerdict.blockedN,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.hard_stop",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})

		case loopVerdict.blocked:
			c.loggerWithTurn(sessionID, requestID).Debug(
				"tool loop breaker: blocked pending justification",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
				"blocked", loopVerdict.blockedN,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.blocked",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})

		case loopVerdict.justified:
			c.loggerWithTurn(sessionID, requestID).Debug(
				"tool loop breaker: justified override accepted",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.justified",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})
		}

		// Lifecycle event: tool execution is about to start.
		c.publishPluginLifecycleEvent("tool_execution_start", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeToolExec{
				BeforeToolExec: &api.ToolCallPayload{
					ToolName:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
					CallId:    tc.ID,
				},
			},
		})

		// Mutation hook: plugins may block the tool or modify its arguments.
		beforeResp := c.dispatchPluginRequestResponse("before_tool_exec", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeToolExec{
				BeforeToolExec: &api.ToolCallPayload{
					ToolName:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
					CallId:    tc.ID,
				},
			},
		})

		// Determine the result, applying permission gates and argument rewriting.
		// The started event fires AFTER mutation hooks so it reflects effective args.
		var result tools.Result
		var toolErr error

		bridge := &loggingUIBridge{
			UIBridge:  c.uiBridge,
			sessionID: sessionID,
			requestID: requestID,
			callID:    tc.ID,
			c:         c,
		}

		// Resolve which tool.Execute call (if any) this turn needs, without
		// running it yet, so the started event — and the "pending" ->
		// "running" transition it drives in the UI — fires before a
		// long-running tool actually executes rather than after it
		// returns (previously the whole call sat in "pending" for its
		// entire duration).
		var run func() (tools.Result, error)
		switch {
		case loopVerdict.blocked || loopVerdict.hardStop:
			result = tools.Result{Content: loopVerdict.message, IsError: true}

		case beforeResp != nil && beforeResp.GetBlockToolExecution():
			reason := beforeResp.GetBlockReason()
			if reason == "" {
				reason = "tool execution blocked by plugin"
			}
			result = tools.Result{Content: reason, IsError: true}

		case beforeResp != nil && beforeResp.GetModifiedToolArguments() != "":
			if !json.Valid([]byte(beforeResp.GetModifiedToolArguments())) {
				result = tools.Result{
					Content: "plugin returned invalid modified_tool_arguments (not valid JSON)",
					IsError: true,
				}
			} else {
				effectiveArgs = beforeResp.GetModifiedToolArguments()
				if tool, ok := c.registry.Get(tc.Function.Name); ok {
					run = func() (tools.Result, error) {
						return tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
					}
				} else {
					result = tools.Result{
						Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
						IsError: true,
					}
				}
			}

		default:
			if tool, ok := c.registry.Get(tc.Function.Name); ok {
				run = func() (tools.Result, error) {
					return tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
				}
			} else {
				result = tools.Result{
					Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
					IsError: true,
				}
			}
		}

		// Emit started event with effective args AFTER plugin hooks but
		// BEFORE tool.Execute runs, so the UI reflects "running" for the
		// actual duration of the call instead of only after it completes.
		c.emit(chat.ChatToolExecutionStartedEvent{
			SessionID:        sessionID,
			RequestID:        requestID,
			CallID:           tc.ID,
			ToolName:         tc.Function.Name,
			ArgumentsSummary: summarizeForUI(effectiveArgs),
			StartedAt:        startedAt,
		})

		if run != nil {
			result, toolErr = run()
			if toolErr != nil {
				result = tools.Result{
					Content: fmt.Sprintf("tool execution error: %v", toolErr),
					IsError: true,
				}
			}
		}

		// Mutation hook: plugins may modify the result. Dispatch on all paths.
		afterResp := c.dispatchPluginRequestResponse("after_tool_exec", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterToolExec{
				AfterToolExec: &api.ToolResultPayload{
					ToolName:  tc.Function.Name,
					Arguments: effectiveArgs,
					Result:    result.Content,
					IsError:   result.IsError,
					CallId:    tc.ID,
				},
			},
		})
		if afterResp != nil && afterResp.GetModifiedToolResult() != "" {
			result.Content = afterResp.GetModifiedToolResult()
		}

		// Preserve the original size before truncating model-visible content.
		if result.ResultBytes == 0 {
			result.ResultBytes = len(result.Content)
		}

		// Truncate after plugin modifications.
		tr := tools.TruncateHead(result.Content, tools.DefaultMaxLines, tools.DefaultMaxBytes)
		result.Content = tr.Content
		result.StartedAt = startedAt
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(startedAt)
		result.Truncated = result.Truncated || tr.Truncated
		if result.IsError && result.ErrorKind == "" {
			result.ErrorKind = toolErrorKind(tc.Function.Name, result.Content)
		}

		results[i] = result
		c.loggerWithTurn(sessionID, requestID).Debug(
			"tool executed",
			"tool", tc.Function.Name,
			"call_id", tc.ID,
			"is_error", result.IsError,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"result_bytes", len(result.Content),
		)
		c.emitToolCompleted(sessionID, requestID, tc, result)

		// Skill activations via the LLM-invoked "skill" tool (the dominant
		// path) need their own metric; handleRunSkill covers the user-command
		// slash command path.
		if tc.Function.Name == "skill" && !result.IsError {
			skillName := tc.Function.Name // fallback
			if args := strings.TrimSpace(tc.Function.Arguments); args != "" {
				var skillArgs struct {
					Name string `json:"name"`
				}
				if json.Unmarshal([]byte(args), &skillArgs) == nil && skillArgs.Name != "" {
					skillName = skillArgs.Name
				}
			}
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategorySkill,
				Name:      "skill.activated",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"skill_name": skillName},
				SessionID: sessionID,
			})
		}

		c.publishPluginLifecycleEvent("tool_execution_end", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterToolExec{
				AfterToolExec: &api.ToolResultPayload{
					ToolName: tc.Function.Name,
					CallId:   tc.ID,
				},
			},
		})
	}

	if c.parallelToolCalls {
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc chat.ChatToolCall) {
				defer wg.Done()
				executeTool(i, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range calls {
			executeTool(i, tc)
		}
	}

	return results, hardStopReason
}

// SetAllowedTools sets the allowed tool filter for the next LLM call.
// When non-empty, only tools whose names are in the set are included in
// the tool schemas sent to the LLM. The "skill" tool is always allowed so
// that the model can switch skills mid-conversation.
func (c *Coordinator) SetAllowedTools(toolNames []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(toolNames) == 0 {
		c.allowedTools = nil
		return
	}
	m := make(map[string]bool, len(toolNames)+1)
	for _, name := range toolNames {
		m[name] = true
	}
	// Always allow switching skills.
	m["skill"] = true
	c.allowedTools = m
}

func (c *Coordinator) buildToolDefs() []chat.ChatToolDef {
	schemas := c.registry.Schemas()

	c.mu.Lock()
	allowed := c.allowedTools
	c.mu.Unlock()

	var filtered []tools.Schema
	if len(allowed) == 0 {
		filtered = schemas
	} else {
		for _, s := range schemas {
			if allowed[s.Name] {
				filtered = append(filtered, s)
			}
		}
	}

	defs := make([]chat.ChatToolDef, len(filtered))
	for i, s := range filtered {
		defs[i] = chat.ChatToolDef{
			Type: "function",
			Function: chat.ChatToolDefFunction{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  s.Parameters,
			},
		}
	}
	return defs
}

func mergeToolCallDelta(calls []chat.ChatToolCall, delta chat.ChatToolCallDelta) []chat.ChatToolCall {
	for len(calls) <= delta.Index {
		calls = append(calls, chat.ChatToolCall{Type: "function"})
	}

	tc := &calls[delta.Index]
	if delta.ID != "" {
		tc.ID = delta.ID
	}
	if delta.Type != "" {
		tc.Type = delta.Type
	}
	if delta.Function.Name != "" {
		// The function name arrives whole in a single delta for every provider
		// observed so far (unlike Arguments, which streams token-by-token) —
		// some providers (e.g. deepseek) resend the full name on every
		// subsequent delta for the same call rather than sending it once, so
		// this must replace, not accumulate, or the name duplicates once per
		// delta (e.g. "shell" -> "shellshellshell...").
		tc.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		tc.Function.Arguments += delta.Function.Arguments
	}
	return calls
}

// sanitizeToolCallArguments checks each tool call's arguments for valid JSON.
// Malformed arguments are replaced with "{}" so they don't poison the session
// history and cause downstream API 400 errors on subsequent turns. Returns the
// number of arguments that were sanitized.
func sanitizeToolCallArguments(calls []chat.ChatToolCall) int {
	sanitized := 0
	for i := range calls {
		args := strings.TrimSpace(calls[i].Function.Arguments)
		if args == "" {
			calls[i].Function.Arguments = "{}"
			sanitized++
			continue
		}
		if !json.Valid([]byte(args)) {
			calls[i].Function.Arguments = "{}"
			sanitized++
		}
	}
	return sanitized
}

func (c *Coordinator) emitReasoningDelta(sessionID, requestID, delta, snapshot string, at time.Time) {
	c.emit(chat.ChatReasoningDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
}

// emitToolCompleted always renders tool results as a text summary via
// summarizeForUI, even for plugin-provided tools - ExecuteToolResponse has
// no View field. The chat.Widget/ExtensionView schema (see
// ExtensionCommandResultEvent) is intentionally reusable here later without
// a breaking wire change, but that's out of scope for this phase.
func (c *Coordinator) emitToolCompleted(
	sessionID string,
	requestID string,
	tc chat.ChatToolCall,
	result tools.Result,
) {
	status := "success"
	if result.IsError {
		status = "error"
	}
	event := chat.ChatToolExecutionCompletedEvent{
		SessionID:     sessionID,
		RequestID:     requestID,
		CallID:        tc.ID,
		ToolName:      tc.Function.Name,
		Status:        status,
		Duration:      result.Duration,
		ResultSummary: summarizeForUI(result.Content),
		IsError:       result.IsError,
		Truncated:     result.Truncated,
		CompletedAt:   result.CompletedAt,
		Details:       result.Details,
	}
	c.emit(event)
	state := c.getSessionState(sessionID)
	metricLabels := make(map[string]string, len(result.MetricLabels)+9)
	for key, value := range result.MetricLabels {
		metricLabels[key] = value
	}
	// Authoritative correlation fields are assigned last so tool-specific
	// dimensions cannot override execution identity or status.
	for key, value := range map[string]string{
		"tool":         tc.Function.Name,
		"status":       status,
		"call_id":      tc.ID,
		"request_id":   requestID,
		"error_kind":   result.ErrorKind,
		"truncated":    strconv.FormatBool(result.Truncated),
		"result_bytes": strconv.Itoa(result.ResultBytes),
		"provider":     state.ProviderName,
		"model":        state.Model.ID,
	} {
		metricLabels[key] = value
	}
	c.emitMetrics(chat.MetricEvent{
		Category:  chat.MetricCategoryTool,
		Name:      "tool." + tc.Function.Name + ".duration",
		Value:     float64(event.Duration.Milliseconds()),
		Unit:      "ms",
		Labels:    metricLabels,
		SessionID: sessionID,
		RequestID: requestID,
		CallID:    tc.ID,
	})
}

func summarizeForUI(value string) string {
	summary, _ := summarizeForUIWithTruncation(value)
	return summary
}

func summarizeForUIWithTruncation(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= toolSummaryMaxBytes {
		return value, false
	}
	return value[:toolSummaryMaxBytes] + "…", true
}

// isSuccessFinish returns true for finish reasons that indicate a normal, successful
// completion. All other reasons (including empty, "error", "content_filter",
// "length", "cancelled") are treated as error outcomes for metric purposes.
func isSuccessFinish(reason string) bool {
	return reason == "stop" || reason == "tool_calls"
}

// computeCost calculates the USD cost of a completion from model pricing
// and usage data. Returns 0 when pricing is not configured.
func computeCost(costCfg tauconfig.CostConfig, usage chat.ChatUsage) float64 {
	if costCfg.Input == 0 && costCfg.Output == 0 {
		return 0
	}
	inputCost := float64(usage.PromptTokens) * costCfg.Input / chat.CostPerMillionTokens
	outputCost := float64(usage.CompletionTokens) * costCfg.Output / chat.CostPerMillionTokens
	return inputCost + outputCost
}

// emitMetrics publishes a MetricEvent on the bus. It is a no-op when no
// subscribers are listening for MetricEvent, avoiding allocation on the
// hot path (tool execution, LLM response).
func (c *Coordinator) emitMetrics(e chat.MetricEvent) {
	if c.metricsPub == nil || !c.metricsPub.ShouldPublish() {
		return
	}
	e.Timestamp = time.Now().UTC()
	c.metricsPub.Publish(e)
}

// Session state helpers — these operate under the coordinator mutex.

func (c *Coordinator) appendDelta(sessionID, requestID, delta string, at time.Time) error {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return errors.New("session not found")
	}
	if session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return context.Canceled
	}
	if err := session.state.AppendAssistantDelta(delta, at); err != nil {
		c.mu.Unlock()
		return err
	}
	snapshot := session.state.PendingAssistant
	c.mu.Unlock()

	c.emit(chat.ChatResponseDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
	return nil
}

func (c *Coordinator) getPendingContent(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return ""
	}
	return session.state.PendingAssistant
}

func (c *Coordinator) commitAssistantMessage(sessionID string, content string, reasoningContent string, calls []chat.ChatToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	_ = session.state.AppendAssistantToolCallMessageWithReasoning(content, reasoningContent, calls, time.Now().UTC())
}

func (c *Coordinator) appendToolResult(sessionID, requestID string, tc chat.ChatToolCall, result tools.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	status := "success"
	if result.IsError {
		status = "error"
	}
	metadata := &chat.ToolResultMetadata{
		ToolName: tc.Function.Name, RequestID: requestID, Status: status,
		ErrorKind: result.ErrorKind, Duration: result.Duration,
		Truncated: result.Truncated, ResultBytes: result.ResultBytes,
		StartedAt: result.StartedAt, CompletedAt: result.CompletedAt,
	}
	_ = session.state.AppendToolResultMessageWithMetadata(tc.ID, result.Content, metadata, result.CompletedAt)
}

func toolErrorKind(toolName, content string) string {
	lower := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.HasPrefix(lower, "[timeout after"):
		return "timeout"
	case strings.HasPrefix(lower, "[exit code:"):
		return "command_exit"
	case strings.Contains(lower, "unknown tool"):
		return "unknown_tool"
	case strings.Contains(lower, "blocked by plugin"):
		return "blocked"
	case strings.Contains(lower, "invalid parameter"), strings.Contains(lower, " is required"), strings.Contains(lower, "must not be empty"):
		return "invalid_arguments"
	case strings.Contains(lower, "escapes working directory"):
		return "sandbox_escape"
	case toolName == "edit" && strings.Contains(lower, "old_text"):
		return "stale_edit"
	default:
		return "execution_error"
	}
}

func (c *Coordinator) clearPending(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	session.state.ClearPendingAssistant(time.Now().UTC())
}

func (c *Coordinator) getSessionState(sessionID string) chat.ChatSessionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return chat.ChatSessionState{}
	}
	return chat.CloneChatSessionState(session.state)
}

func (c *Coordinator) completeTurn(sessionID, requestID string, result chat.CompletionResult, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	turnStartedAt := session.turnStartedAt
	_ = session.state.CompleteTurnWithReasoning(result.FinishReason, result.Usage, result.ReasoningContent, at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn completed",
		"finish_reason", result.FinishReason,
		"total_tokens", result.Usage.TotalTokens,
		"duration_ms", time.Since(turnStartedAt).Milliseconds(),
	)

	// Emit LLM latency metric when we have a turn start timestamp.
	if !turnStartedAt.IsZero() {
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryLLM,
			Name:     "turn.duration",
			Value:    float64(time.Since(turnStartedAt).Milliseconds()),
			Unit:     "ms",
			Labels: map[string]string{
				"provider": snapshot.Provider.Name,
				"model":    snapshot.Model.ID,
			},
			SessionID: sessionID,
		})
	}

	// Emit error metric for non-success finish reasons.  error_kind
	// differentiates between cancellation, content filtering, length
	// truncation, and provider-specific refusals (e.g. Gemini safety).
	if !isSuccessFinish(result.FinishReason) {
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryLLM,
			Name:     "llm.error",
			Value:    1,
			Unit:     "count",
			Labels: map[string]string{
				"provider":      snapshot.Provider.Name,
				"model":         snapshot.Model.ID,
				"finish_reason": result.FinishReason,
			},
			SessionID: sessionID,
		})
	}

	// Emit LLM response metrics with token counts and cost.
	cost := computeCost(snapshot.Model.Config.Cost, result.Usage)
	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.response",
		Value:    float64(result.Usage.TotalTokens),
		Unit:     "tokens",
		Labels: map[string]string{
			"provider":          snapshot.Provider.Name,
			"model":             snapshot.Model.ID,
			"prompt_tokens":     fmt.Sprintf("%d", result.Usage.PromptTokens),
			"completion_tokens": fmt.Sprintf("%d", result.Usage.CompletionTokens),
			"finish_reason":     result.FinishReason,
		},
		SessionID: sessionID,
	})
	if cost > 0 {
		c.emitMetrics(chat.MetricEvent{
			Category:  chat.MetricCategoryLLM,
			Name:      "llm.cost",
			Value:     cost,
			Unit:      "usd",
			Labels:    map[string]string{"provider": snapshot.Provider.Name, "model": snapshot.Model.ID},
			SessionID: sessionID,
		})
	}

	c.emit(chat.ChatResponseCompletedEvent{
		State:        snapshot,
		RequestID:    requestID,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		CompletedAt:  at,
	})

	c.publishPluginLifecycleEvent("turn_end", sessionID, &api.EventPayload{
		Kind: &api.EventPayload_Turn{Turn: &api.TurnPayload{Direction: "end"}},
	})
}

func (c *Coordinator) cancelTurn(sessionID, requestID string, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.CancelTurn(at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn cancelled",
		"provider", snapshot.Provider.Name,
		"model", snapshot.Model.ID,
	)

	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.error",
		Value:    1,
		Unit:     "count",
		Labels: map[string]string{
			"provider":      snapshot.Provider.Name,
			"model":         snapshot.Model.ID,
			"finish_reason": "cancelled",
		},
		SessionID: sessionID,
	})

	c.emit(chat.ChatResponseCancelledEvent{
		State:       snapshot,
		RequestID:   requestID,
		CancelledAt: at,
	})
}

func (c *Coordinator) failTurn(sessionID, requestID string, err error, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.FailTurn(err, at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Warn(
		"turn failed",
		"provider", snapshot.Provider.Name,
		"model", snapshot.Model.ID,
		"err", err,
	)

	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.error",
		Value:    1,
		Unit:     "count",
		Labels: map[string]string{
			"provider":      snapshot.Provider.Name,
			"model":         snapshot.Model.ID,
			"finish_reason": "error",
			"error_kind":    "stream_error",
		},
		SessionID: sessionID,
	})

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emit(chat.ChatRuntimeErrorEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Message:    err.Error(),
		Fatal:      false,
		OccurredAt: at,
	})
}

func (c *Coordinator) cancelAllSessions() {
	c.mu.Lock()
	states := make([]chat.ChatSessionState, 0, len(c.sessions))
	sessionIDs := make([]string, 0, len(c.sessions))
	for id, session := range c.sessions {
		if session.cancel != nil {
			session.cancel()
		}
		states = append(states, chat.CloneChatSessionState(session.state))
		sessionIDs = append(sessionIDs, id)
	}
	c.mu.Unlock()

	for _, state := range states {
		sessionID := state.SessionID
		// Dedup shutdown events (preserving existing dedup logic).
		c.mu.Lock()
		if _, exists := c.shutdown[sessionID]; exists {
			c.mu.Unlock()
			c.persistSession(state, time.Since(state.CreatedAt))
			continue
		}
		c.shutdown[sessionID] = struct{}{}
		c.mu.Unlock()
		c.publishPluginLifecycleEvent("session_shutdown", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{
				SessionId: sessionID,
				ModelId:   state.Model.ID,
				Provider:  state.Provider.Name,
			}},
		})
		c.persistSession(state, time.Since(state.CreatedAt))
	}

	// Release session and shutdown map entries after all sessions are persisted.
	c.mu.Lock()
	for _, id := range sessionIDs {
		delete(c.sessions, id)
	}
	c.shutdown = make(map[string]struct{})
	c.mu.Unlock()
}

// marshalMessages serializes messages for plugin event payloads.
func marshalMessages(msgs []chat.ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		b, _ := json.Marshal(m)
		out[i] = string(b)
	}
	return out
}

// marshalParameters serializes chat parameters for plugin event payloads.
func marshalParameters(p chat.ChatParameters) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// applyPluginMessageModifications applies message injections and removals from a
// plugin EventResponse to the provided session state. It processes removals in
// descending index order to avoid index shifting, then appends injected messages.
// Malformed injected messages are skipped rather than failing the turn.
func (c *Coordinator) applyPluginMessageModifications(state *chat.ChatSessionState, resp *api.EventResponse) {
	if resp == nil || state == nil {
		return
	}
	// Process removals in descending order to keep indices stable.
	indices := make([]int32, len(resp.GetRemoveMessageIndices()))
	copy(indices, resp.GetRemoveMessageIndices())
	sort.Slice(indices, func(i, j int) bool { return indices[i] > indices[j] })
	for _, idx := range indices {
		if int(idx) >= 0 && int(idx) < len(state.Messages) {
			state.Messages = append(state.Messages[:idx], state.Messages[idx+1:]...)
		}
	}
	// Inject messages.
	for _, raw := range resp.GetInjectMessages() {
		var msg chat.ChatMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			c.loggerWith(state.SessionID).Warn(
				"failed to decode injected message",
				"err", err,
			)
			continue
		}
		state.Messages = append(state.Messages, msg)
	}
}

// dispatchPluginRequestResponse dispatches a request-response lifecycle
// event to plugins via the configured callback. Use publishPluginLifecycleEvent
// for fire-and-forget notifications that don't need a response.
func (c *Coordinator) dispatchPluginRequestResponse(event string, sessionID string, payload *api.EventPayload) *api.EventResponse {
	if c.onPluginEvent == nil {
		return nil
	}
	return c.onPluginEvent(event, sessionID, payload)
}

// publishPluginLifecycleEvent publishes a fire-and-forget lifecycle
// notification on the event bus. Plugin manager (or any subscriber)
// receives these asynchronously.
func (c *Coordinator) publishPluginLifecycleEvent(event, sessionID string, payload *api.EventPayload) {
	c.loggerWith(sessionID).Debug(
		"plugin lifecycle event emitted",
		"event", event,
		"payload_kind", pluginPayloadKind(payload),
	)
	c.pluginPub.Publish(chat.PluginLifecycleEvent{
		Event:     event,
		SessionID: sessionID,
		Payload:   payload,
	})
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

func normalizedTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

// --- Session persistence ---

func (c *Coordinator) handleListSessions(cmd chat.ListSessionsCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Session persistence is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	summaries, nextCursor, err := c.sessionManager.List(ctx, cmd.Limit, cmd.Cursor)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("listing sessions: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	c.emit(chat.SessionsListedEvent{
		Sessions:   summaries,
		NextCursor: nextCursor,
	})
}

func (c *Coordinator) handleLoadSession(cmd chat.LoadSessionCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	// Capture template session config under lock before async I/O.
	c.mu.Lock()
	templateSession := c.sessions[cmd.RuntimeSessionID]
	if templateSession == nil {
		templateSession = c.sessions[cmd.SessionID]
	}
	var runtimeCfg *sessions.RuntimeSessionConfig
	if templateSession != nil {
		runtimeCfg = &sessions.RuntimeSessionConfig{
			Provider:    templateSession.state.Provider,
			ModelID:     templateSession.state.Model.ID,
			ModelURL:    templateSession.state.Model.URL,
			ModelConfig: templateSession.state.Model.Config,
			Parameters:  templateSession.state.Parameters,
		}
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	loaded, err := c.sessionManager.Load(ctx, cmd.SessionID, runtimeCfg)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    fmt.Sprintf("loading session: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	c.mu.Lock()
	if current := c.sessions[cmd.SessionID]; current != nil && current.cancel != nil {
		current.cancel()
	}
	c.sessions[cmd.SessionID] = &coordinatorSession{state: &loaded}
	c.mu.Unlock()

	c.emit(chat.SessionLoadedEvent{State: loaded})
}

func (c *Coordinator) handleDeleteSession(cmd chat.DeleteSessionCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	if err := c.sessionManager.Delete(ctx, cmd.SessionID); err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    fmt.Sprintf("deleting session: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	// Remove from in-memory session map to free associated state.
	c.mu.Lock()
	if s, exists := c.sessions[cmd.SessionID]; exists {
		if s.cancel != nil {
			s.cancel()
		}
		delete(c.sessions, cmd.SessionID)
	}
	delete(c.shutdown, cmd.SessionID)
	c.mu.Unlock()

	c.emit(chat.SessionDeletedEvent(cmd))
}

func (c *Coordinator) handleExportSession(cmd chat.ExportSessionCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	outputPath := cmd.Output
	if outputPath != "" {
		ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
		defer cancel()

		if err := c.sessionManager.ExportToJSONL(ctx, cmd.SessionID, outputPath); err != nil {
			c.emit(chat.ChatRuntimeErrorEvent{
				SessionID:  cmd.SessionID,
				Message:    fmt.Sprintf("exporting session: %v", err),
				Fatal:      false,
				OccurredAt: time.Now().UTC(),
			})
			return
		}
	} else {
		// Export to stdout: stream lines through events.
		ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
		defer cancel()

		ch, errCh := c.sessionManager.ExportMessages(ctx, cmd.SessionID)

		var output strings.Builder
		for line := range ch {
			output.Write(line)
		}

		select {
		case err := <-errCh:
			if err != nil {
				c.emit(chat.ChatRuntimeErrorEvent{
					SessionID:  cmd.SessionID,
					Message:    fmt.Sprintf("exporting session: %v", err),
					Fatal:      false,
					OccurredAt: time.Now().UTC(),
				})
				return
			}
		default:
		}

		// Write to actual stdout for CLI exports.
		fmt.Fprint(os.Stdout, output.String())
	}

	c.emit(chat.SessionExportedEvent{
		SessionID: cmd.SessionID,
		Format:    cmd.Format,
		Path:      outputPath,
	})
}

// persistSession saves the session state to the store. It is called on graceful
// close and on forced shutdown. Errors are logged but not surfaced to the TUI —
// persistence is best-effort.
func (c *Coordinator) persistSession(state chat.ChatSessionState, duration time.Duration) {
	if c.sessionManager == nil || c.noPersist {
		return
	}

	// Deliberately not c.ctx: this runs from cancelAllSessions on the
	// c.ctx.Done() shutdown path, so a context derived from c.ctx would
	// already be cancelled and the final save would never happen.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.sessionManager.Save(ctx, state, duration); err != nil {
		c.loggerWith(state.SessionID).Error(
			"persist session failed",
			"err", err,
		)
		return
	}

	c.loggerWith(state.SessionID).Debug(
		"session persisted",
		"msg_count", len(state.Messages),
		"duration_ms", duration.Milliseconds(),
	)

	if !c.autoExportJSONL {
		return
	}

	// Auto-export JSONL as a background convenience artifact.
	go func() {
		exportPath := c.sessionManager.SessionJSONLPath(state.SessionID, state.CreatedAt)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.sessionManager.ExportToJSONL(ctx, state.SessionID, exportPath); err != nil {
			c.loggerWith(state.SessionID).Warn(
				"auto-export jsonl failed",
				"err", err,
			)
		}
	}()
}

type loggingUIBridge struct {
	tools.UIBridge
	sessionID string
	requestID string
	callID    string
	c         *Coordinator
}

func (b *loggingUIBridge) Log(chunk string) {
	b.c.emit(chat.ChatToolOutputEvent{
		SessionID:  b.sessionID,
		RequestID:  b.requestID,
		CallID:     b.callID,
		Chunk:      chunk,
		ReceivedAt: time.Now().UTC(),
	})
}
