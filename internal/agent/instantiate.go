package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/procid"
	"github.com/samcharles93/tau/internal/store"
)

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
	// PreResolvedRootDef, when set, is used directly instead of
	// re-resolving the root spec. The root startup path resolves once via
	// ResolveRootSpec to apply the root-spec override trust gate (G14; see
	// docs/specs/agents/01-agent-spec-format.md, Root-spec override trust)
	// before instantiation - this avoids a second, potentially
	// inconsistent, discovery pass and the TOCTOU window that would open
	// between the trust check and the actual instantiation. Ignored for
	// children (ParentInstanceID != "").
	PreResolvedRootDef *spec.Definition
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
	if cfg.ParentInstanceID == "" && cfg.PreResolvedRootDef != nil {
		def = cfg.PreResolvedRootDef
	} else if name == "tau" && cfg.ParentInstanceID == "" {
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
		def.Provider,
		cfg.InheritedProvider,
		cfg.InheritedModel,
		cfg.DefaultProvider,
		cfg.DefaultModel,
		cfg.ModelModes,
	)

	// Step 3: Compute the effective toolset.
	effectiveTools := computeEffectiveTools(def.Tools, cfg)

	// Step 4: mint the instance id and the instance row (ID is minted
	// below, with collision retry).
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
	specSnapshot := spec.BuildSpecSnapshot(def, resolvedProvider, resolvedModel, effectiveTools)
	specHash := spec.HashSpecSnapshot(specSnapshot)

	now := time.Now()
	inst := store.AgentInstance{
		SpecName:         def.Name,
		SpecScope:        spec.ScopeString(def.Scope),
		SpecSourcePath:   def.SourcePath,
		SpecHash:         specHash,
		SpecSnapshot:     specSnapshot,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   spec.ToolsToJSON(effectiveTools),
		Depth:            depth,
		ParentInstanceID: cfg.ParentInstanceID,
		PID:              osPID(),
		ProcessStartNS:   procid.CaptureProcessStartNS(osPID()),
		StartedAt:        now,
	}

	// Retry with a freshly minted ID on a primary-key collision. 6-char
	// base32 gives ~30 bits of entropy - rare, but the DB is the
	// authoritative uniqueness check, not the RNG (see
	// docs/specs/agents/04-storage-and-sessions.md, G3/G10).
	instanceID := ""
	var saveErr error
	for range spec.MaxInstanceIDCollisionRetries {
		inst.ID = spec.MintInstanceID(def.Name)
		if saveErr = cfg.Store.SaveAgentInstance(ctx, inst); saveErr == nil {
			instanceID = inst.ID
			break
		}
		if !store.IsUniqueConstraintError(saveErr) {
			return nil, fmt.Errorf("instantiate: save instance: %w", saveErr)
		}
	}
	if instanceID == "" {
		return nil, fmt.Errorf("instantiate: save instance: id collision after %d attempts: %w", spec.MaxInstanceIDCollisionRetries, saveErr)
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

// ResolveRootSpec resolves the root agent's spec exactly as Instantiate
// does internally for the bare name "tau" (project > user > built-in full
// discovery). Exported so the root startup path can inspect the resolution
// - specifically its Scope and SourcePath - to apply the root-spec
// override trust gate (G14) before instantiation, then pass the same
// *spec.Definition back in via InstantiateConfig.PreResolvedRootDef.
func ResolveRootSpec(cwd string) *spec.Definition {
	return resolveTauRoot(cwd)
}

// ResolveBuiltinTau resolves the built-in "tau" spec directly, bypassing
// filesystem discovery. Used as the fallback when a project-level root-spec
// override is rejected by the trust gate (G14) - the spec's documented
// "N: reject. Fall back to the built-in tau spec."
func ResolveBuiltinTau() *spec.Definition {
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
		// Parent is unrestricted - child gets spec ∩ spawn only.
		return intersectTools(specTools, cfg.SpawnTools)
	}

	// Parent has restrictions - intersect all three.
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

// buildSpecSnapshot serialises the resolved definition + body as JSON
// so every instance carries exactly what ran it.

// hashSpec returns the hex-encoded SHA-256 of the spec snapshot JSON.
// Hashing the full snapshot (which includes all frontmatter fields and the
// resolved model/tools) means any change to tools:, model:, description, or
// the body will produce a different hash - correctly detecting spec drift
// rather than silently colliding on identical bodies with different frontmatter.

// toolsToJSON serialises a tool list as a JSON array string, or "" for nil/empty.

// defaultOrphanStaleAge is used when staleAge <= 0 (config unset).
const defaultOrphanStaleAge = 24 * time.Hour

// SweepOrphanedInstances closes any agent_instances row (at any depth) with
// ended_at IS NULL whose owning process is no longer running, or which has
// simply run too long to be trusted. Called at root startup. Implements the
// sweep algorithm from docs/specs/agents/04-storage-and-sessions.md (Orphan
// sweep): the stale-age bound is checked first and closes unconditionally;
// pid == 0 (never recorded - e.g. the process crashed before
// SetAgentInstancePID ran) closes immediately; otherwise a platform PID
// check with process-start identity decides. A row is never closed on
// indeterminate evidence (permission denied, TOCTOU) - it's left for the
// next sweep or the stale-age bound to eventually resolve.
func SweepOrphanedInstances(ctx context.Context, s store.SessionStore, ownPID int, staleAge time.Duration) error {
	if staleAge <= 0 {
		staleAge = defaultOrphanStaleAge
	}
	insts, err := s.ListOpenAgentInstances(ctx)
	if err != nil {
		return fmt.Errorf("sweep: list open instances: %w", err)
	}
	now := time.Now()
	for _, inst := range insts {
		if inst.PID == ownPID {
			continue // this process's own row - never sweep it
		}
		if now.Sub(inst.StartedAt) > staleAge {
			_ = s.CloseAgentInstance(ctx, inst.ID, "failed", "", "orphan sweep: stale age exceeded")
			continue
		}
		if inst.PID <= 0 {
			_ = s.CloseAgentInstance(ctx, inst.ID, "failed", "", "orphan sweep: no pid recorded")
			continue
		}
		switch procid.CheckPIDIdentity(inst.PID, inst.ProcessStartNS) {
		case procid.PIDCheckAlive:
			// Process is alive and identity matches (or no identity could
			// be recorded) - skip, it may still be doing useful work.
		case procid.PIDCheckDead:
			_ = s.CloseAgentInstance(ctx, inst.ID, "failed", "", "orphan sweep: pid not found or identity mismatch")
		case procid.PIDCheckIndeterminate:
			slog.Warn("orphan sweep: could not determine pid liveness, skipping", "instance_id", inst.ID, "pid", inst.PID)
		}
	}
	return nil
}

// osPID returns the current OS process id.
func osPID() int {
	return os.Getpid()
}
