package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/store"
)

// AgentToolConfig holds the dependencies the agent tool executor needs.
type AgentToolConfig struct {
	// CWD is the working directory for child processes.
	CWD string
	// Store is the shared session/instance store.
	Store store.SessionStore
	// ModelModes is the user-configured tier map.
	ModelModes map[string]config.ModeConfig
	// DefaultProvider / DefaultModel are the global config defaults.
	DefaultProvider string
	DefaultModel    string
	// Agents holds the default depth/cap config.
	Agents config.AgentsConfig
	// ParentInstanceID is this coordinator's agent instance address.
	ParentInstanceID string
	// ParentDepth is this coordinator's depth in the agent tree.
	ParentDepth int
	// ParentEffectiveTools is this coordinator's effective toolset.
	// nil means unrestricted.
	ParentEffectiveTools []string
	// InheritedProvider / InheritedModel are this coordinator's resolved pair.
	InheritedProvider string
	InheritedModel    string
	// TauPath is the path to the tau binary for spawning children.
	// Empty means os.Executable().
	TauPath string
	// Bus is the parent coordinator's event bus for forwarding child
	// events and publishing usage updates.
	Bus *eventbus.Bus
	// SessionID is the parent session that owns this spawn call, used
	// to scope forwarded events on the parent bus.
	SessionID string
	// ParentMessages is the current conversation history, used when
	// context_mode is "fork". nil if unavailable.
	ParentMessages []tauchat.ChatMessage
	// ParentSystemPrompt is the parent session's system prompt, used
	// when context_mode is "fork" to seed the child.
	ParentSystemPrompt string
}

// NewAgentTool creates the agent tool for spawning child agent processes.
func NewAgentTool(cfg AgentToolConfig) Tool {
	return Tool{
		Schema: Schema{
			Name:        "agent",
			Description: agentToolDescription,
			Parameters:  agentToolParams,
		},
		Execute: func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error) {
			return executeAgentTool(ctx, params, ui, cfg)
		},
		Source: "builtin",
	}
}

const agentToolDescription = `Spawn a child agent to delegate a task to. Use this when:
- The task is large enough to benefit from a dedicated agent with its own toolset and context.
- You need a specialist agent (e.g. research for deep exploration, plan for structured planning).
- The work can run in parallel with other work (multiple agent calls in one turn run concurrently).

Choose the target agent by name based on its description. Use the default "tau" agent for general-purpose delegation. Prefer fresh context unless the child genuinely needs the full parent conversation history.`

var agentToolParams = mustMarshal(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"agent": map[string]any{
			"type":        "string",
			"description": "The agent spec to spawn: a built-in name (research, plan, tau, task) or a prefixed spec (user:name, project:name).",
		},
		"prompt": map[string]any{
			"type":        "string",
			"description": "The task for the child agent to complete.",
		},
		"context": map[string]any{
			"type":        "string",
			"description": "Optional context string passed to the child as a <parent_context> block. Ignored when context_mode is fork.",
		},
		"context_mode": map[string]any{
			"type":        "string",
			"enum":        []string{"fresh", "fork"},
			"description": "How to seed the child's session. 'fresh' (default) starts a new session with the system prompt and task. 'fork' copies the full parent conversation history.",
		},
		"resume": map[string]any{
			"type":        "string",
			"description": "Session ID of a previously finished child session to continue. Mutually exclusive with context/context_mode.",
		},
		"model": map[string]any{
			"type":        "string",
			"description": "Tier name (fast, smart, deep) or concrete model. Overrides the spec's model. Precedence: spawn > spec > inherit.",
		},
		"tools": map[string]any{
			"type":        "array",
			"items":       map[string]string{"type": "string"},
			"description": "Optional list of tool names to further narrow the child's toolset. Can only restrict, never add tools the child wouldn't already have.",
		},
		"budget": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"max_tokens": map[string]any{
					"type":        "integer",
					"description": "Maximum total tokens the child may consume before returning partial results.",
				},
				"deadline": map[string]any{
					"type":        "string",
					"description": "Wall-clock deadline (e.g. '5m', '1h'). The child returns what it has when the deadline fires.",
				},
			},
			"description": "Optional budget caps for this specific task.",
		},
	},
	"required": []string{"agent", "prompt"},
})

func executeAgentTool(ctx context.Context, params json.RawMessage, _ UIBridge, cfg AgentToolConfig) (Result, error) {
	var args struct {
		Agent       string   `json:"agent"`
		Prompt      string   `json:"prompt"`
		Context     string   `json:"context"`
		ContextMode string   `json:"context_mode"`
		Resume      string   `json:"resume"`
		Model       string   `json:"model"`
		Tools       []string `json:"tools"`
		Budget      *struct {
			MaxTokens int    `json:"max_tokens"`
			Deadline  string `json:"deadline"`
		} `json:"budget"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid agent call parameters: %v", err), IsError: true, ErrorKind: "invalid_params"}, nil
	}

	// ---- Pre-spawn checks ----

	// 1. Resolve the spec.
	targetName := strings.TrimSpace(args.Agent)
	if targetName == "" {
		return Result{Content: "agent call failed: spec name is required", IsError: true, ErrorKind: "spec_not_found"}, nil
	}
	def, ok := spec.Resolve(targetName, cfg.CWD)
	if !ok {
		return Result{Content: fmt.Sprintf("agent call failed: spec %q not found", targetName), IsError: true, ErrorKind: "spec_not_found"}, nil
	}

	// 2. Reject disable-model-invocation specs.
	if def.DisableModelInvocation {
		return Result{Content: fmt.Sprintf("agent call failed: spec %q is not spawnable (disable-model-invocation: true)", targetName), IsError: true, ErrorKind: "not_spawnable"}, nil
	}

	// 3. Depth cap check.
	childDepth := cfg.ParentDepth + 1
	maxDepth := cfg.Agents.DefaultMaxDepth
	if maxDepth <= 0 {
		maxDepth = config.DefaultAgentsConfig().DefaultMaxDepth
	}
	ceiling := cfg.Agents.DepthCeiling
	if ceiling <= 0 {
		ceiling = config.DefaultAgentsConfig().DepthCeiling
	}
	if maxDepth > 0 && childDepth > maxDepth {
		return Result{Content: fmt.Sprintf("agent call failed: depth %d exceeds cap %d", childDepth, maxDepth), IsError: true, ErrorKind: "depth_exceeded"}, nil
	}
	if ceiling > 0 && childDepth > ceiling {
		return Result{Content: fmt.Sprintf("agent call failed: depth %d exceeds ceiling %d", childDepth, ceiling), IsError: true, ErrorKind: "depth_exceeded"}, nil
	}

	// 4. Resume mutual exclusion and session validation.
	if args.Resume != "" {
		if args.Context != "" || args.ContextMode != "" {
			return Result{Content: "agent call failed: 'resume' is mutually exclusive with 'context'/'context_mode'", IsError: true, ErrorKind: "invalid_params"}, nil
		}
		// Validate the resume session exists.
		if cfg.Store == nil {
			return Result{Content: "agent call failed: resume requires a session store", IsError: true, ErrorKind: "invalid_params"}, nil
		}
		_, err := cfg.Store.Load(context.Background(), args.Resume)
		if err != nil {
			return Result{Content: fmt.Sprintf("agent call failed: resume session %q not found", args.Resume), IsError: true, ErrorKind: "session_not_found"}, nil
		}
	}

	// 5. Instantiate the child agent.
	instCfg := instantiateConfig{
		Name:                 targetName,
		CWD:                  cfg.CWD,
		ParentInstanceID:     cfg.ParentInstanceID,
		ParentDepth:          cfg.ParentDepth,
		ParentEffectiveTools: cfg.ParentEffectiveTools,
		SpawnTools:           args.Tools,
		ModelOverride:        args.Model,
		InheritedProvider:    cfg.InheritedProvider,
		InheritedModel:       cfg.InheritedModel,
		ModelModes:           cfg.ModelModes,
		DefaultProvider:      cfg.DefaultProvider,
		DefaultModel:         cfg.DefaultModel,
		Agents:               cfg.Agents,
		Store:                cfg.Store,
	}

	instResult, err := instantiateChild(ctx, instCfg)
	if err != nil {
		return Result{Content: fmt.Sprintf("agent instantiation failed: %v", err), IsError: true, ErrorKind: "instantiation_failed"}, nil
	}

	// 5b. Seed the child session based on context_mode.
	if err := seedChildSession(ctx, seedSessionConfig{
		Store:              cfg.Store,
		SessionID:          instResult.SessionID,
		ParentSessionID:    cfg.SessionID,
		ParentMessages:     cfg.ParentMessages,
		ParentSystemPrompt: cfg.ParentSystemPrompt,
		ContextMode:        args.ContextMode,
		Resume:             args.Resume,
		Prompt:             args.Prompt,
		Context:            args.Context,
		SpecName:           args.Agent,
	}); err != nil {
		return Result{Content: fmt.Sprintf("seed child session: %v", err), IsError: true, ErrorKind: "instantiation_failed"}, nil
	}

	// 6. Spawn the child process.
	tauPath := cfg.TauPath
	if tauPath == "" {
		tauPath, _ = exec.LookPath("tau") // Best effort — let exec fail if not found.
	}

	cmd := exec.CommandContext(ctx, tauPath, "--child")
	cmd.Dir = cfg.CWD
	cmd.Env = nil // inherit
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return agentToolError("stdin pipe", err), nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agentToolError("stdout pipe", err), nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return agentToolError("stderr pipe", err), nil
	}
	// Drain stderr to slog in background.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				fmt.Printf("[child %s] %s", instResult.InstanceID, string(buf[:n]))
			}
			if readErr != nil {
				break
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		return agentToolError("start child", err), nil
	}

	// 7. Handshake: read agent.ready, write agent.assign.
	childReader := stdio.NewReader(stdout)
	childWriter := stdio.NewWriter(stdin)

	ready, err := childReader.ReadHandshake(stdio.ProtocolVersion)
	if err != nil {
		cmd.Process.Kill()
		_ = cmd.Wait()
		return agentToolError("handshake", err), nil
	}
	_ = ready // PID is available if needed.

	// Build limits from the resolved spec (structural caps).
	limits := bridge.AgentLimits{MaxTurns: def.MaxTurns}
	if def.Timeout != 0 {
		limits.Timeout = def.Timeout.String()
	}

	// Build budget from the spawn call, converting the deadline from a
	// relative duration (the tool-facing schema) to an absolute RFC3339
	// timestamp (the wire contract child.go expects).
	var budget bridge.AgentBudget
	if args.Budget != nil {
		budget.MaxTokens = args.Budget.MaxTokens
		if args.Budget.Deadline != "" {
			d, err := time.ParseDuration(args.Budget.Deadline)
			if err != nil {
				return Result{
					Content:   fmt.Sprintf("invalid budget.deadline %q: %v", args.Budget.Deadline, err),
					IsError:   true,
					ErrorKind: "invalid_params",
				}, nil
			}
			budget.Deadline = time.Now().Add(d).Format(time.RFC3339)
		}
	}

	assignMsg := bridge.AgentAssign{
		TaskID:     instResult.InstanceID,
		InstanceID: instResult.InstanceID,
		SessionID:  instResult.SessionID,
		Prompt:     args.Prompt,
		Context:    args.Context,
		Model: bridge.AgentModelPair{
			Provider: instResult.ResolvedProvider,
			Model:    instResult.ResolvedModel,
		},
		Tools:  instResult.EffectiveTools,
		Depth:  instResult.Depth,
		Limits: limits,
		Budget: budget,
	}
	if err := childWriter.WriteEnvelope("agent.assign", assignMsg); err != nil {
		cmd.Process.Kill()
		_ = cmd.Wait()
		return agentToolError("write assign", err), nil
	}

	// 8. Stream events and wait for agent.result.
	// Create a dedicated bus client for this child so forwarded events are
	// scoped and don't leak between concurrent children.
	startedAt := time.Now()
	childBusClient := cfg.Bus.Client("agent-" + instResult.InstanceID)
	childPub := eventbus.Publish[tauchat.ChatEvent](childBusClient)

	finalText, resultEnv, usageAcc, err := readChildResult(childReader, cmd, instResult.InstanceID, cfg.SessionID, childPub)
	if err != nil {
		cmd.Process.Kill()
		_ = cmd.Wait()
		childBusClient.Close()
		return agentToolError("child result", err), nil
	}

	// 9. Wait for the child to exit. A non-zero exit with an already-successful
	// result still counts as failed — the child exited abnormally after sending
	// its result, which means the result may be incomplete.
	exitErr := cmd.Wait()
	if exitErr != nil && resultEnv.Status == "completed" {
		resultEnv.Status = "failed"
		resultEnv.Error = fmt.Sprintf("child exited abnormally: %v", exitErr)
		resultEnv.Partial = true
	}

	// 10. Close the instance row and bus client.
	if cfg.Store != nil && usageAcc != nil {
		usageJSON, _ := json.Marshal(usageAcc)
		_ = cfg.Store.CloseAgentInstance(ctx, instResult.InstanceID, resultEnv.Status, string(usageJSON))
	}
	childBusClient.Close()

	// 11. Assemble the tool result per the completion contract
	// (docs/specs/agents/02-spawning-and-lifecycle.md).
	elapsed := time.Since(startedAt)
	content := finalText
	if resultEnv.Status != "completed" {
		content = assembleStatusLine(finalText, resultEnv.Status, resultEnv.Usage.Turns, instResult.InstanceID, elapsed)
	}

	detailsJSON, _ := json.Marshal(map[string]any{
		"status":      resultEnv.Status,
		"instance_id": instResult.InstanceID,
		"session_id":  instResult.SessionID,
		"usage": map[string]any{
			"turns":         resultEnv.Usage.Turns,
			"input_tokens":  resultEnv.Usage.InputTokens,
			"output_tokens": resultEnv.Usage.OutputTokens,
			"cost":          resultEnv.Usage.Cost,
		},
		"error":   resultEnv.Error,
		"partial": resultEnv.Partial,
	})

	return Result{
		Content: content,
		Details: detailsJSON,
		IsError: resultEnv.Status == "failed",
	}, nil
}

// instantiateConfig mirrors agent.InstantiateConfig but is defined here
// to avoid importing internal/agent from the tools package (tools is a leaf).
type instantiateConfig struct {
	Name                 string
	CWD                  string
	ParentInstanceID     string
	ParentDepth          int
	ParentEffectiveTools []string
	SpawnTools           []string
	ModelOverride        string
	InheritedProvider    string
	InheritedModel       string
	ModelModes           map[string]config.ModeConfig
	DefaultProvider      string
	DefaultModel         string
	Agents               config.AgentsConfig
	Store                store.SessionStore
}

type instantiateResult struct {
	InstanceID       string
	SessionID        string
	ResolvedProvider string
	ResolvedModel    string
	EffectiveTools   []string
	Depth            int
}

// instantiateChild resolves, attenuates, mints and persists a child instance.
// It mirrors agent.Instantiate but uses the local instantiateConfig to avoid
// importing the agent package (tools is a leaf package).
func instantiateChild(ctx context.Context, cfg instantiateConfig) (*instantiateResult, error) {
	def, ok := spec.Resolve(cfg.Name, cfg.CWD)
	if !ok {
		return nil, fmt.Errorf("spec %q not found", cfg.Name)
	}

	resolvedProvider, resolvedModel := config.ResolveModelMode(
		cfg.ModelOverride,
		def.Model,
		def.Provider,
		cfg.InheritedProvider,
		cfg.InheritedModel,
		cfg.DefaultProvider,
		cfg.DefaultModel,
		cfg.ModelModes,
	)

	effectiveTools := computeChildEffectiveTools(def.Tools, cfg.ParentEffectiveTools, cfg.SpawnTools)

	instanceID := spec.MintInstanceID(def.Name)
	depth := cfg.ParentDepth + 1

	// Depth enforcement (same as agent.Instantiate).
	maxDepth := cfg.Agents.DefaultMaxDepth
	if maxDepth <= 0 {
		maxDepth = config.DefaultAgentsConfig().DefaultMaxDepth
	}
	ceiling := cfg.Agents.DepthCeiling
	if ceiling <= 0 {
		ceiling = config.DefaultAgentsConfig().DepthCeiling
	}
	if maxDepth > 0 && depth > maxDepth {
		return nil, fmt.Errorf("depth %d exceeds cap %d", depth, maxDepth)
	}
	if ceiling > 0 && depth > ceiling {
		return nil, fmt.Errorf("depth %d exceeds ceiling %d", depth, ceiling)
	}

	// Mint a session ID for the child.
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("mint session id: %w", err)
	}

	now := time.Now()
	specSnapshot := spec.BuildSpecSnapshot(def, resolvedProvider, resolvedModel, effectiveTools)
	inst := store.AgentInstance{
		ID:               instanceID,
		SpecName:         def.Name,
		SpecScope:        spec.ScopeString(def.Scope),
		SpecSourcePath:   def.SourcePath,
		SpecHash:         spec.HashSpecSnapshot(specSnapshot),
		SpecSnapshot:     specSnapshot,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   spec.ToolsToJSON(effectiveTools),
		Depth:            depth,
		ParentInstanceID: cfg.ParentInstanceID,
		StartedAt:        now,
	}

	if cfg.Store != nil {
		if err := cfg.Store.SaveAgentInstance(ctx, inst); err != nil {
			return nil, fmt.Errorf("save instance: %w", err)
		}
	}

	return &instantiateResult{
		InstanceID:       instanceID,
		SessionID:        sessionID,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   effectiveTools,
		Depth:            depth,
	}, nil
}

// computeChildEffectiveTools computes the child's effective toolset via
// attenuation: child spec ∩ parent effective ∩ spawn narrowing.
func computeChildEffectiveTools(specTools, parentEffective, spawnTools []string) []string {
	step1 := intersectToolLists(specTools, parentEffective)
	return intersectToolLists(step1, spawnTools)
}

func intersectToolLists(a, b []string) []string {
	if len(a) == 0 {
		return append([]string(nil), b...)
	}
	if len(b) == 0 {
		return append([]string(nil), a...)
	}
	set := make(map[string]bool, len(b))
	for _, name := range b {
		set[name] = true
	}
	var out []string
	for _, name := range a {
		if set[name] {
			out = append(out, name)
		}
	}
	return out
}

// seedSessionConfig holds the parameters for seeding a child session.
type seedSessionConfig struct {
	Store              store.SessionStore
	SessionID          string
	ParentSessionID    string
	ParentMessages     []tauchat.ChatMessage
	ParentSystemPrompt string
	ContextMode        string // "fresh" (default), "fork"
	Resume             string // session ID to resume
	Prompt             string
	Context            string
	SpecName           string
}

// seedChildSession creates or seeds a child session based on context_mode.
//   - fresh (default): creates new ChatSessionState with rendered spec body
//     as system prompt, task prompt as user message, and context as
//     <parent_context> block. parent_session_id is set.
//   - fork: clones parent session history, appends task prompt as new user
//     message. parent_session_id is set.
//   - resume: no new session; the child runs against the existing child
//     session. No session creation happens.
//
// Mutual-exclusion (resume vs context/context_mode) is already validated
// upstream in executeAgentTool.
func seedChildSession(ctx context.Context, cfg seedSessionConfig) error {
	// Resume: no session to create — child loads the existing session.
	if cfg.Resume != "" {
		return nil
	}

	// Default context_mode is "fresh".
	mode := cfg.ContextMode
	if mode == "" {
		mode = "fresh"
	}

	switch mode {
	case "fresh":
		return seedFreshSession(ctx, cfg)
	case "fork":
		return seedForkSession(ctx, cfg)
	default:
		return fmt.Errorf("unknown context_mode: %q", mode)
	}
}

// seedFreshSession creates a new child session with the rendered spec body
// as the system prompt, the task prompt as the user message, and the
// optional context string as a <parent_context> block.
func seedFreshSession(ctx context.Context, cfg seedSessionConfig) error {
	now := time.Now()
	state := tauchat.ChatSessionState{
		SessionID:       cfg.SessionID,
		ParentSessionID: cfg.ParentSessionID,
		Status:          tauchat.ChatSessionIdle,
		CreatedAt:       now,
		UpdatedAt:       now,
		SystemPrompt:    cfg.ParentSystemPrompt,
		Messages: []tauchat.ChatMessage{
			{
				Role:      tauchat.ChatRoleUser,
				Content:   buildChildPrompt(cfg.Prompt, cfg.Context),
				CreatedAt: now,
			},
		},
	}
	return cfg.Store.Save(ctx, state, 0)
}

// seedForkSession clones the parent's full session history and appends the
// task prompt as the next user message in the child's session.
func seedForkSession(ctx context.Context, cfg seedSessionConfig) error {
	now := time.Now()
	parent := tauchat.ChatSessionState{
		SessionID:    cfg.ParentSessionID,
		Messages:     cfg.ParentMessages,
		SystemPrompt: cfg.ParentSystemPrompt,
	}
	clone := tauchat.CloneChatSessionState(&parent)
	clone.SessionID = cfg.SessionID
	clone.ParentSessionID = cfg.ParentSessionID
	clone.CreatedAt = now
	clone.UpdatedAt = now
	clone.Status = tauchat.ChatSessionIdle
	clone.Messages = append(clone.Messages, tauchat.ChatMessage{
		Role:      tauchat.ChatRoleUser,
		Content:   cfg.Prompt,
		CreatedAt: now,
	})

	return cfg.Store.Save(ctx, clone, 0)
}

// buildChildPrompt wraps the task prompt with an optional <parent_context>
// block for fresh sessions.
func buildChildPrompt(prompt, ctxStr string) string {
	if strings.TrimSpace(ctxStr) == "" {
		return prompt
	}
	return fmt.Sprintf("<parent_context>\n%s\n</parent_context>\n\n%s", ctxStr, prompt)
}

// readChildResult reads envelopes from the child's stdout until it sees
// agent.result, forwarding agent.event to the parent bus and accumulating
// agent.usage totals. Returns the child's final text, the result envelope,
// accumulated usage, and any error.
func readChildResult(reader *stdio.Reader, cmd *exec.Cmd, instanceID, parentSessionID string, childPub *eventbus.Publisher[tauchat.ChatEvent]) (string, *bridge.AgentResult, *bridge.AgentResultUsage, error) {
	var finalText strings.Builder
	var usageAcc bridge.AgentResultUsage

	for {
		typ, payload, err := reader.ReadEnvelope()
		if err != nil {
			// EOF without agent.result — child crashed. Synthesise failed.
			exitErr := cmd.Wait()
			errDetail := "child exited without result"
			if exitErr != nil {
				errDetail = fmt.Sprintf("child exited without result: %v", exitErr)
			}
			return finalText.String(), &bridge.AgentResult{
				TaskID:    instanceID,
				Status:    "failed",
				FinalText: finalText.String(),
				Partial:   true,
				Error:     errDetail,
				Usage: bridge.AgentResultUsage{
					Turns:        usageAcc.Turns,
					InputTokens:  usageAcc.InputTokens,
					OutputTokens: usageAcc.OutputTokens,
					Cost:         usageAcc.Cost,
				},
			}, &usageAcc, nil
		}

		switch typ {
		case "agent.event":
			// Forward child events to the parent bus. We publish via
			// ChatToolOutputEvent carrying the raw inner event envelope.
			if childPub != nil {
				childPub.Publish(tauchat.ChatToolOutputEvent{
					SessionID:  parentSessionID,
					CallID:     instanceID,
					Chunk:      string(payload),
					ReceivedAt: time.Now(),
				})
			}
		case "agent.usage":
			var usage bridge.AgentResultUsage
			if err := json.Unmarshal(payload, &usage); err != nil {
				continue
			}
			// Cumulative — last message wins.
			if usage.Turns > usageAcc.Turns {
				usageAcc = usage
			}
		case "agent.result":
			var result bridge.AgentResult
			if err := json.Unmarshal(payload, &result); err != nil {
				return finalText.String(), nil, &usageAcc, fmt.Errorf("unmarshal agent.result: %w", err)
			}
			result.TaskID = instanceID
			// Backfill usage if the child didn't report its own.
			if result.Usage.Turns == 0 {
				result.Usage = usageAcc
			}
			return finalText.String(), &result, &usageAcc, nil
		default:
			_ = typ // ignore unknown envelopes
		}
	}
}

func agentToolError(stage string, err error) Result {
	return Result{
		Content:   fmt.Sprintf("agent call failed (%s): %v", stage, err),
		IsError:   true,
		ErrorKind: "agent_tool_error",
	}
}

// assembleStatusLine builds the text the parent model sees for abnormal
// (non-completed) child results. On abnormal end the parent model receives
// the partial text (if any) plus a compact harness-appended status line.
// See docs/specs/agents/02-spawning-and-lifecycle.md (Completion contract).
func assembleStatusLine(finalText, status string, turns int, instanceID string, elapsed time.Duration) string {
	if status == "completed" {
		return finalText
	}
	dur := formatDuration(elapsed)
	partial := ""
	if status != "completed" && finalText != "" {
		partial = "; partial output above"
	}
	line := fmt.Sprintf("[agent %s ended: %s after %s, %d turns%s]",
		instanceID, status, dur, turns, partial)
	if finalText == "" {
		return line
	}
	return finalText + "\n\n" + line
}

// formatDuration formats a duration compactly, e.g. "5m", "1h2m", "30s".
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// scopeStringStatic returns the string representation of a skills.Scope.
func generateSessionID() (string, error) {
	return fmt.Sprintf("child-%d", time.Now().UnixNano()), nil
}

func mustMarshal(v map[string]any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

var _ = errors.New // keep import for future use
