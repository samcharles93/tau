// Package instance implements the dependency-neutral agent instantiation
// semantics shared by root and child callers.
package instance

import (
	"context"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

// Config holds the resolved inputs needed to instantiate an agent. Spec
// discovery and trust decisions remain with the caller.
type Config struct {
	Child                bool
	Definition           *spec.Definition
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
	PID                  int
	ProcessStartNS       int64

	// MintInstanceID is a test seam. Production callers leave it nil.
	MintInstanceID func(string) string
}

// Result contains the resolved identity and runtime limits for a new instance.
type Result struct {
	InstanceID       string
	SpecName         string
	ResolvedProvider string
	ResolvedModel    string
	EffectiveTools   []string
	Depth            int
	MaxTurns         int
	Timeout          time.Duration
}

// Instantiate resolves model, depth, tools, snapshot, and persistent identity
// for an already-resolved agent definition.
func Instantiate(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.Definition == nil {
		return nil, fmt.Errorf("definition is required")
	}
	def := cfg.Definition

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

	effectiveTools := EffectiveTools(
		def.Tools,
		cfg.Child,
		cfg.ParentEffectiveTools,
		cfg.SpawnTools,
	)

	depth := cfg.ParentDepth
	if cfg.Child {
		depth++
	}
	if depth > 0 {
		if err := CheckDepth(depth, cfg.Agents); err != nil {
			return nil, err
		}
	}

	snapshot := spec.BuildSpecSnapshot(def, resolvedProvider, resolvedModel, effectiveTools)
	inst := store.AgentInstance{
		SpecName:         def.Name,
		SpecScope:        spec.ScopeString(def.Scope),
		SpecSourcePath:   def.SourcePath,
		SpecHash:         spec.HashSpecSnapshot(snapshot),
		SpecSnapshot:     snapshot,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   spec.ToolsToJSON(effectiveTools),
		Depth:            depth,
		ParentInstanceID: cfg.ParentInstanceID,
		PID:              cfg.PID,
		ProcessStartNS:   cfg.ProcessStartNS,
		StartedAt:        time.Now(),
	}

	instanceID, err := PersistWithIDRetry(&inst, def.Name, cfg.MintInstanceID, func(inst store.AgentInstance) error {
		if cfg.Store == nil {
			return nil
		}
		return cfg.Store.SaveAgentInstance(ctx, inst)
	})
	if err != nil {
		return nil, fmt.Errorf("save instance: %w", err)
	}

	return &Result{
		InstanceID:       instanceID,
		SpecName:         def.Name,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   effectiveTools,
		Depth:            depth,
		MaxTurns:         def.MaxTurns,
		Timeout:          def.Timeout,
	}, nil
}

// ResumeConfig holds the authoritative historical instance plus the current
// parent's restrictions for transferring an ended session to a new child.
type ResumeConfig struct {
	Original             store.AgentInstance
	SessionID            string
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
	MintInstanceID       func(string) string
}

// Resume recomputes a resumed child's model, capabilities, limits, snapshot,
// and identity before atomically transferring session ownership.
func Resume(ctx context.Context, cfg ResumeConfig) (*Result, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("session store is required")
	}

	depth := cfg.ParentDepth + 1
	if err := CheckDepth(depth, cfg.Agents); err != nil {
		return nil, err
	}

	resolvedProvider := cfg.Original.ResolvedProvider
	resolvedModel := cfg.Original.ResolvedModel
	if cfg.ModelOverride != "" {
		resolvedProvider, resolvedModel = config.ResolveModelMode(
			cfg.ModelOverride,
			"",
			"",
			cfg.InheritedProvider,
			cfg.InheritedModel,
			cfg.DefaultProvider,
			cfg.DefaultModel,
			cfg.ModelModes,
		)
	}

	originalCeiling := spec.ToolsFromJSON(cfg.Original.EffectiveTools)
	effectiveTools := EffectiveTools(
		originalCeiling,
		true,
		cfg.ParentEffectiveTools,
		cfg.SpawnTools,
	)
	snapshot, err := spec.PatchSnapshotResolved(
		cfg.Original.SpecSnapshot,
		resolvedProvider,
		resolvedModel,
		effectiveTools,
	)
	if err != nil {
		return nil, fmt.Errorf("patch snapshot: %w", err)
	}
	maxTurns, timeout := spec.SnapshotLimits(snapshot)

	inst := store.AgentInstance{
		SpecName:         cfg.Original.SpecName,
		SpecScope:        cfg.Original.SpecScope,
		SpecSourcePath:   cfg.Original.SpecSourcePath,
		SpecHash:         spec.HashSpecSnapshot(snapshot),
		SpecSnapshot:     snapshot,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   spec.ToolsToJSON(effectiveTools),
		Depth:            depth,
		ParentInstanceID: cfg.ParentInstanceID,
		StartedAt:        time.Now(),
	}
	instanceID, err := PersistWithIDRetry(&inst, cfg.Original.SpecName, cfg.MintInstanceID, func(inst store.AgentInstance) error {
		return cfg.Store.ResumeSession(ctx, cfg.SessionID, inst)
	})
	if err != nil {
		return nil, err
	}

	return &Result{
		InstanceID:       instanceID,
		SpecName:         cfg.Original.SpecName,
		ResolvedProvider: resolvedProvider,
		ResolvedModel:    resolvedModel,
		EffectiveTools:   effectiveTools,
		Depth:            depth,
		MaxTurns:         maxTurns,
		Timeout:          timeout,
	}, nil
}

// PersistWithIDRetry assigns an instance ID and invokes persist, retrying
// unique-constraint collisions with freshly minted IDs.
func PersistWithIDRetry(
	inst *store.AgentInstance,
	specName string,
	mintID func(string) string,
	persist func(store.AgentInstance) error,
) (string, error) {
	mint := mintID
	if mint == nil {
		mint = spec.MintInstanceID
	}
	var lastErr error
	for range spec.MaxInstanceIDCollisionRetries {
		inst.ID = mint(specName)
		if err := persist(*inst); err != nil {
			if store.IsUniqueConstraintError(err) {
				lastErr = err
				continue
			}
			return "", err
		}
		return inst.ID, nil
	}
	return "", fmt.Errorf(
		"id collision after %d attempts: %w",
		spec.MaxInstanceIDCollisionRetries,
		lastErr,
	)
}

// EffectiveTools applies root selection or child attenuation. A nil or empty
// contributor means unrestricted.
func EffectiveTools(specTools []string, child bool, parentEffective, spawnTools []string) []string {
	if !child {
		return cloneTools(specTools)
	}
	return intersectTools(intersectTools(specTools, parentEffective), spawnTools)
}

func intersectTools(a, b []string) []string {
	if len(a) == 0 {
		return cloneTools(b)
	}
	if len(b) == 0 {
		return cloneTools(a)
	}
	set := make(map[string]struct{}, len(b))
	for _, name := range b {
		set[name] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, name := range a {
		if _, ok := set[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

func cloneTools(tools []string) []string {
	if len(tools) == 0 {
		return nil
	}
	return append([]string(nil), tools...)
}

// CheckDepth enforces the configured default cap and hard ceiling.
func CheckDepth(depth int, agentsCfg config.AgentsConfig) error {
	maxDepth := agentsCfg.DefaultMaxDepth
	if maxDepth <= 0 {
		maxDepth = config.DefaultAgentsConfig().DefaultMaxDepth
	}
	ceiling := agentsCfg.DepthCeiling
	if ceiling <= 0 {
		ceiling = config.DefaultAgentsConfig().DepthCeiling
	}
	if maxDepth > 0 && depth > maxDepth {
		return fmt.Errorf("depth %d exceeds cap %d", depth, maxDepth)
	}
	if ceiling > 0 && depth > ceiling {
		return fmt.Errorf("depth %d exceeds ceiling %d", depth, ceiling)
	}
	return nil
}
