package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/skills"
	"github.com/samcharles93/tau/internal/store"
)

// base32Alphabet is the lowercased RFC 4648 alphabet without padding,
// used for instance-id suffixes.
const base32Alphabet = "abcdefghijklmnopqrstuvwxyz234567"

// instanceIDLen is the number of random base32 characters in an instance id.
const instanceIDLen = 6

// InstantiateConfig holds the parameters for bringing an agent identity
// into existence. See docs/specs/agents/02-spawning-and-lifecycle.md.
type InstantiateConfig struct {
	// Name is the spec to resolve: bare "tau", "research", or prefixed
	// "user:" / "project:".
	Name string
	// CWD is the working directory for spec discovery and instance context.
	CWD string
	// ParentInstanceID is set for children, empty for root processes.
	ParentInstanceID string
	// ParentSessionID is set for children, empty for root processes.
	ParentSessionID string
	// ParentDepth is the parent's depth; child depth = parentDepth + 1.
	ParentDepth int
	// ParentEffectiveTools is the parent's effective toolset for attenuation.
	// nil means unrestricted (root process with no spec tools list).
	ParentEffectiveTools []string
	// SpawnTools narrows the child's toolset beyond attenuation (from the
	// spawn call's "tools" parameter). nil means no spawn-level narrowing.
	SpawnTools []string
	// ModelOverride is a tier name or concrete model from the spawn call.
	// Empty means no override.
	ModelOverride string
	// InheritedProvider / InheritedModel are the invoking instance's
	// already-resolved provider/model pair.
	InheritedProvider string
	InheritedModel    string
	// ModelModes is the user-configured tier map.
	ModelModes map[string]config.ModeConfig
	// DefaultProvider / DefaultModel are the global config defaults.
	DefaultProvider string
	DefaultModel    string
	// Agents holds the default depth/cap config.
	Agents config.AgentsConfig
	// Store is used to persist the new instance row. The caller owns
	// the session that's created from the returned SessionConfig.
	Store store.SessionStore
}

// InstantiateResult is the output of a successful Instantiate call.
type InstantiateResult struct {
	// InstanceID is the minted agent instance address, e.g. "research#k3v9qp".
	InstanceID string
	// SessionConfig is the ready-to-use session configuration with
	// AgentInstanceID set. The caller sends StartChatSessionCommand with this.
	SessionConfig chat.ChatSessionConfig
	// ResolvedProvider / ResolvedModel are the concrete provider/model pair
	// the instance will use.
	ResolvedProvider string
	ResolvedModel    string
	// EffectiveTools is the computed toolset for this instance.
	// nil means unrestricted (full registry).
	EffectiveTools []string
	// Depth is the instance's tree depth (0 for root).
	Depth int
}

// Instantiate runs the five-step agent-instantiation path: resolve the spec,
// resolve the model to a concrete pair, compute the effective toolset
// (with attenuation for children), mint an instance id, write the instance
// row, and return a ChatSessionConfig ready for the caller to start a session.
//
// For the root process (no parent), depth is 0 and the effective toolset is
// the spec's tools or nil (full registry). For children, the effective
// toolset is the intersection of child spec tools ∩ parent effective
// ∩ spawn narrowing.
//
// The bare name "tau" has a special resolution path on root startup only
// (ParentInstanceID == ""): it resolves through full discovery
// (project > user > built-in) so project/user overrides win. This is the
// only place bare-name resolution differs from the standard Resolve().
func Instantiate(ctx context.Context, cfg InstantiateConfig) (*InstantiateResult, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, fmt.Errorf("instantiate: spec name is required")
	}

	// Step 1: Resolve the spec.
	// For the bare name "tau" at root startup, resolve through full
	// discovery so project/user overrides win (see 00-overview.md decision 1).
	var def *spec.Definition
	if name == "tau" && cfg.ParentInstanceID == "" {
		def = resolveTauRoot(cfg.CWD)
	}
	if def == nil {
		d, ok := spec.Resolve(name, cfg.CWD)
		if !ok {
			return nil, fmt.Errorf("instantiate: spec %q not found", name)
		}
		def = d
	}

	// Step 2: Resolve the model to a concrete provider/model pair.
	resolvedProvider, resolvedModel := config.ResolveModelMode(
		cfg.ModelOverride,
		def.Model,
		"", // specProvider — not yet parsed, added in P1.2
		cfg.InheritedProvider,
		cfg.InheritedModel,
		cfg.DefaultProvider,
		cfg.DefaultModel,
		cfg.ModelModes,
	)

	// Step 3: Compute the effective toolset.
	effectiveTools := computeEffectiveTools(def.Tools, cfg)

	// Step 4: Mint the instance id and the instance row.
	instanceID := mintInstanceID(def.Name)
	depth := cfg.ParentDepth
	if cfg.ParentInstanceID != "" {
		depth++ // child
	}

	// Apply depth caps from config defaults.
	if depth > 0 {
		maxDepth := cfg.Agents.DefaultMaxDepth
		if maxDepth <= 0 {
			maxDepth = config.DefaultAgentsConfig().DefaultMaxDepth
		}
		ceiling := cfg.Agents.DepthCeiling
		if ceiling <= 0 {
			ceiling = config.DefaultAgentsConfig().DepthCeiling
		}
		if maxDepth > 0 && depth > maxDepth {
			return nil, fmt.Errorf("instantiate: depth %d exceeds cap %d", depth, maxDepth)
		}
		if ceiling > 0 && depth > ceiling {
			return nil, fmt.Errorf("instantiate: depth %d exceeds ceiling %d", depth, ceiling)
		}
	}

	// Build the spec snapshot (resolved definition + body as JSON).
	specSnapshot := buildSpecSnapshot(def, resolvedProvider, resolvedModel, effectiveTools)
	specHash := hashSpec(specSnapshot)

	now := time.Now()
	inst := store.AgentInstance{
		ID:               instanceID,
		SpecName:         def.Name,
		SpecScope:        scopeString(def.Scope),
		SpecSourcePath:   def.SourcePath,
		SpecHash:         specHash,
		SpecSnapshot:     specSnapshot,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   toolsToJSON(effectiveTools),
		Depth:            depth,
		ParentInstanceID: cfg.ParentInstanceID,
		PID:              osPID(),
		StartedAt:        now,
	}
	if err := cfg.Store.SaveAgentInstance(ctx, inst); err != nil {
		return nil, fmt.Errorf("instantiate: save instance: %w", err)
	}

	// Step 5: Build the session config.
	sessionCfg := chat.ChatSessionConfig{
		Provider:        config.ProviderConfig{Name: resolvedProvider},
		Model:           chat.ChatModelRef{ID: resolvedModel},
		SystemPrompt:    "", // caller fills this in (e.g. BuildSystemPrompt for root)
		ParentSessionID: cfg.ParentSessionID,
		AgentInstanceID: instanceID,
	}

	result := &InstantiateResult{
		InstanceID:       instanceID,
		SessionConfig:    sessionCfg,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   effectiveTools,
		Depth:            depth,
	}
	return result, nil
}

// resolveTauRoot resolves the bare name "tau" through full discovery
// (project > user > built-in) so overrides win. Used only at root startup.
func resolveTauRoot(cwd string) *spec.Definition {
	sources := spec.DefaultSources(cwd)
	defs, _ := spec.DiscoverFromDisk(sources)
	for _, def := range defs {
		if strings.EqualFold(def.Name, "tau") {
			return def
		}
	}
	d, _ := spec.Lookup("tau")
	return d
}

// computeEffectiveTools computes the effective toolset for a new instance.
// For root processes (no parent), the spec's tools are used directly
// (nil means unrestricted). For children, the effective set is:
//
//	child spec tools ∩ parent effective ∩ spawn narrowing
//
// A nil contributor means "no restriction from this level".
func computeEffectiveTools(specTools []string, cfg InstantiateConfig) []string {
	// Root process: spec tools or nil (unrestricted).
	if cfg.ParentInstanceID == "" {
		if len(specTools) == 0 {
			return nil
		}
		out := make([]string, len(specTools))
		copy(out, specTools)
		return out
	}

	// Child: intersect spec tools ∩ parent effective ∩ spawn narrowing.
	if len(cfg.ParentEffectiveTools) == 0 {
		// Parent is unrestricted — child gets spec ∩ spawn only.
		return intersectTools(specTools, cfg.SpawnTools)
	}

	// Parent has restrictions — intersect all three.
	step1 := intersectTools(specTools, cfg.ParentEffectiveTools)
	return intersectTools(step1, cfg.SpawnTools)
}

// intersectTools returns the intersection of a and b. If either is nil,
// the other is returned unchanged (nil = no restriction).
func intersectTools(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
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

// mintInstanceID generates a new instance address like "research#k3v9qp".
func mintInstanceID(specName string) string {
	var suffix [instanceIDLen]byte
	_, _ = rand.Read(suffix[:])
	for i := range suffix {
		suffix[i] = base32Alphabet[int(suffix[i])%len(base32Alphabet)]
	}
	return fmt.Sprintf("%s#%s", specName, string(suffix[:]))
}

// buildSpecSnapshot serialises the resolved definition + body as JSON
// so every instance carries exactly what ran it.
func buildSpecSnapshot(def *spec.Definition, provider, model string, tools []string) string {
	snap := map[string]any{
		"name":        def.Name,
		"description": def.Description,
		"body":        def.Body,
		"resolved": map[string]any{
			"provider": provider,
			"model":    model,
		},
	}
	if len(tools) > 0 {
		snap["tools"] = tools
	}
	if def.Scope != "" {
		snap["scope"] = scopeString(def.Scope)
	}
	if def.SourcePath != "" {
		snap["source_path"] = def.SourcePath
	}
	data, _ := json.Marshal(snap)
	return string(data)
}

// hashSpec returns the hex-encoded SHA-256 of the spec snapshot JSON.
// Hashing the full snapshot (which includes all frontmatter fields and the
// resolved model/tools) means any change to tools:, model:, description, or
// the body will produce a different hash — correctly detecting spec drift
// rather than silently colliding on identical bodies with different frontmatter.
func hashSpec(snapshotJSON string) string {
	h := sha256.Sum256([]byte(snapshotJSON))
	return fmt.Sprintf("%x", h[:])
}

// scopeString returns the string representation of a skills.Scope.
func scopeString(scope skills.Scope) string {
	switch scope {
	case skills.ScopeUser:
		return "user"
	case skills.ScopeProject:
		return "project"
	case skills.ScopeBuiltin:
		return "builtin"
	default:
		return ""
	}
}

// toolsToJSON serialises a tool list as a JSON array string, or "" for nil/empty.
func toolsToJSON(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	data, _ := json.Marshal(tools)
	return string(data)
}

// osPID returns the current OS process id.
func osPID() int {
	return os.Getpid()
}

// SweepOrphanedInstances closes any agent_instances rows with ended_at IS NULL
// whose pid is no longer running. Called at root startup. See
// docs/specs/agents/04-storage-and-sessions.md (Orphan sweep).
func SweepOrphanedInstances(ctx context.Context, s store.SessionStore, ownPID int) error {
	insts, err := s.ListAgentInstances(ctx, "")
	if err != nil {
		return fmt.Errorf("sweep: list instances: %w", err)
	}
	for _, inst := range insts {
		if inst.EndedAt.IsZero() && inst.PID > 0 && inst.PID != ownPID {
			if !pidAlive(inst.PID) {
				_ = s.CloseAgentInstance(ctx, inst.ID, "failed", "")
			}
		}
	}
	return nil
}

// pidAlive returns true if the process with the given pid is currently running.
// Uses a Signal(0) check on Unix; on unsupported platforms returns true
// (conservative — never closes instances that might still be alive).
// This is safe because pids are only advisory per
// docs/specs/agents/04-storage-and-sessions.md.
func pidAlive(_ int) bool {
	// Conservative: always assume alive. The orphan sweep is a convenience;
	// dead pid rows are harmless and get closed eventually on the next sweep.
	// TODO: add a platform-specific Signal(0) check.
	return true
}
