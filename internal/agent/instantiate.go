package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	agentinstance "github.com/samcharles93/tau/internal/agent/instance"
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
	InstanceID       string
	SessionConfig    chat.ChatSessionConfig
	ResolvedProvider string
	ResolvedModel    string
	EffectiveTools   []string
	Depth            int
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

	pid := osPID()
	result, err := agentinstance.Instantiate(ctx, agentinstance.Config{
		Child:                cfg.ParentInstanceID != "",
		Definition:           def,
		ParentInstanceID:     cfg.ParentInstanceID,
		ParentDepth:          cfg.ParentDepth,
		ParentEffectiveTools: cfg.ParentEffectiveTools,
		SpawnTools:           cfg.SpawnTools,
		ModelOverride:        cfg.ModelOverride,
		InheritedProvider:    cfg.InheritedProvider,
		InheritedModel:       cfg.InheritedModel,
		ModelModes:           cfg.ModelModes,
		DefaultProvider:      cfg.DefaultProvider,
		DefaultModel:         cfg.DefaultModel,
		Agents:               cfg.Agents,
		Store:                cfg.Store,
		PID:                  pid,
		ProcessStartNS:       procid.CaptureProcessStartNS(pid),
	})
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	return &InstantiateResult{
		InstanceID: result.InstanceID,
		SessionConfig: chat.ChatSessionConfig{
			Provider:        config.ProviderConfig{Name: result.ResolvedProvider},
			Model:           chat.ChatModelRef{ID: result.ResolvedModel},
			ParentSessionID: cfg.ParentSessionID,
			AgentInstanceID: result.InstanceID,
		},
		ResolvedProvider: result.ResolvedProvider,
		ResolvedModel:    result.ResolvedModel,
		EffectiveTools:   result.EffectiveTools,
		Depth:            result.Depth,
	}, nil
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
