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
	"strings"
	"sync"
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

	mu       sync.Mutex
	sessions map[string]*coordinatorSession
	shutdown map[string]struct{}

	closeOnce sync.Once
	done      chan struct{}
	loopDone  chan struct{}
	turnWG    sync.WaitGroup
	promptMu  sync.Mutex
	prompts   map[string]chan interactivePromptResponse
	promptSeq uint64
}

type coordinatorSession struct {
	state           *chat.ChatSessionState
	cancel          context.CancelFunc
	steeringMu      sync.Mutex
	pendingSteering []string
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

	ParallelToolCalls *bool // nil → default (true)
	InteractiveUI     bool
	ExtensionReloader chat.ExtensionReloader
	SessionManager    *sessions.Manager
	AutoExportJSONL   bool
	OnClose           func()
	StartupEvents     []chat.ChatEvent

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

	c := &Coordinator{
		ctx:               ctx,
		cancel:            cancel,
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
		slog.Error(
			"coordinator: saving default provider/model to local config",
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
	if cmd.RequestID != "" && session.state.ActiveRequestID != cmd.RequestID {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			RequestID:  cmd.RequestID,
			Message:    "requested turn is not active",
			Fatal:      false,
			OccurredAt: now,
		})
		return
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

// handleReloadSkills re-discovers skills from disk and merges them into the
// command registry, enabling hot-reload without restarting tau.
func (c *Coordinator) handleReloadSkills(cmd chat.ReloadSkillsCommand) {
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
func (c *Coordinator) handleListSkills(cmd chat.ListSkillsCommand) {
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

	// Also show as a notification for backward compatibility
	var b strings.Builder
	b.WriteString("available skills:\n")
	for _, s := range snapshot.ActiveSkills {
		if s == nil {
			continue
		}
		fmt.Fprintf(&b, "  %s — %s\n", s.Name, s.Description)
	}
	c.emit(chat.ChatNotificationEvent{
		Message:    b.String(),
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
		if err != nil {
			// A single user-facing error notification.
			c.emit(chat.ChatNotificationEvent{
				Message:    "Extension command failed: " + err.Error(),
				Level:      chat.ChatNotificationError,
				OccurredAt: at,
			})
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

// runTurn is the agentic turn loop. It streams a completion, and if the
// model returns tool_calls, executes them in parallel, appends results
// to the conversation, and loops. Stops when the model produces a final
// text response or an error occurs.
func (c *Coordinator) runTurn(ctx context.Context, state chat.ChatSessionState) {
	sessionID := state.SessionID
	requestID := state.ActiveRequestID
	now := time.Now().UTC()

	c.publishPluginLifecycleEvent("turn_start", sessionID, &api.EventPayload{
		Kind: &api.EventPayload_Turn{Turn: &api.TurnPayload{Direction: "start"}},
	})

	bearerToken, err := c.tokenSource(ctx, state.Provider)
	if err != nil {
		c.failTurn(sessionID, requestID, err, now)
		return
	}

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
				c.cancelTurn(sessionID, requestID, time.Now().UTC())
				return
			}
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

		// No tool calls → final response. Complete the turn.
		if len(result.ToolCalls) == 0 {
			c.completeTurn(sessionID, requestID, result, time.Now().UTC())
			return
		}

		// Tool calls detected. Validate arguments before committing —
		// malformed JSON in tool call arguments poisons the session history
		// and causes downstream API 400 errors on subsequent turns.
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
		toolResults := c.executeToolsParallel(ctx, sessionID, requestID, result.ToolCalls)

		// Append tool result messages to the session.
		for i, tc := range result.ToolCalls {
			c.appendToolResult(sessionID, tc.ID, toolResults[i].Content)
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

// executeToolsParallel runs tool calls and returns results in input order.
// When parallelToolCalls is true, calls run concurrently; otherwise sequentially.
func (c *Coordinator) executeToolsParallel(ctx context.Context, sessionID, requestID string, calls []chat.ChatToolCall) []tools.Result {
	results := make([]tools.Result, len(calls))

	executeTool := func(i int, tc chat.ChatToolCall) {
		startedAt := time.Now().UTC()
		effectiveArgs := tc.Function.Arguments

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

		switch {
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
				tool, ok := c.registry.Get(tc.Function.Name)
				if !ok {
					result = tools.Result{
						Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
						IsError: true,
					}
				} else {
					result, toolErr = tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
					if toolErr != nil {
						result = tools.Result{
							Content: fmt.Sprintf("tool execution error: %v", toolErr),
							IsError: true,
						}
					}
				}
			}

		default:
			tool, ok := c.registry.Get(tc.Function.Name)
			if !ok {
				result = tools.Result{
					Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
					IsError: true,
				}
			} else {
				result, toolErr = tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
				if toolErr != nil {
					result = tools.Result{
						Content: fmt.Sprintf("tool execution error: %v", toolErr),
						IsError: true,
					}
				}
			}
		}

		// Emit started event with effective args AFTER plugin hooks.
		c.emit(chat.ChatToolExecutionStartedEvent{
			SessionID:        sessionID,
			RequestID:        requestID,
			CallID:           tc.ID,
			ToolName:         tc.Function.Name,
			ArgumentsSummary: summarizeForUI(effectiveArgs),
			StartedAt:        startedAt,
		})

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

		// Truncate after plugin modifications.
		tr := tools.TruncateHead(result.Content, tools.DefaultMaxLines, tools.DefaultMaxBytes)
		result.Content = tr.Content

		results[i] = result
		c.emitToolCompleted(sessionID, requestID, tc, result, startedAt, tr.Truncated)

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

	return results
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
		tc.Function.Name += delta.Function.Name
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
	startedAt time.Time,
	truncated bool,
) {
	completedAt := time.Now().UTC()
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
		Duration:      completedAt.Sub(startedAt),
		ResultSummary: summarizeForUI(result.Content),
		IsError:       result.IsError,
		Truncated:     truncated,
		CompletedAt:   completedAt,
	}
	c.emit(event)
	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryTool,
		Name:     "tool." + tc.Function.Name + ".duration",
		Value:    float64(event.Duration.Milliseconds()),
		Unit:     "ms",
		Labels: map[string]string{
			"tool":   tc.Function.Name,
			"status": status,
		},
		SessionID: sessionID,
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

func (c *Coordinator) appendToolResult(sessionID string, callID, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	_ = session.state.AppendToolResultMessage(callID, content, time.Now().UTC())
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
	_ = session.state.CompleteTurnWithReasoning(result.FinishReason, result.Usage, result.ReasoningContent, at)
	session.cancel = nil
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

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
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

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
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

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
	sessions := make([]*coordinatorSession, 0, len(c.sessions))
	sessionIDs := make([]string, 0, len(c.sessions))
	for id, session := range c.sessions {
		if session.cancel != nil {
			session.cancel()
		}
		sessions = append(sessions, session)
		sessionIDs = append(sessionIDs, id)
	}
	c.mu.Unlock()

	for _, session := range sessions {
		state := chat.CloneChatSessionState(session.state)
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
			slog.Warn("coordinator: failed to decode injected message", "err", err)
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
	c.pluginPub.Publish(chat.PluginLifecycleEvent{
		Event:     event,
		SessionID: sessionID,
		Payload:   payload,
	})
}

func (c *Coordinator) emit(event chat.ChatEvent) {
	c.chatPub.Publish(event)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if c.sessionManager == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.sessionManager.Save(ctx, state, duration); err != nil {
		slog.Error(
			"coordinator: persist session failed",
			"session_id", state.SessionID,
			"err", err,
		)
		return
	}

	if !c.autoExportJSONL {
		return
	}

	// Auto-export JSONL as a background convenience artifact.
	go func() {
		exportPath := c.sessionManager.SessionJSONLPath(state.SessionID, state.CreatedAt)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.sessionManager.ExportToJSONL(ctx, state.SessionID, exportPath); err != nil {
			slog.Warn(
				"coordinator: auto-export jsonl failed",
				"session_id", state.SessionID,
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
