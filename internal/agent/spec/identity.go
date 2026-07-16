package spec

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/skills"
)

// HashSpecSnapshot returns the hex-encoded SHA-256 of a spec snapshot JSON string.
// The snapshot includes all frontmatter fields (tools, model, description), so
// a change to any of them produces a different hash - correctly detecting spec
// drift rather than silently colliding on identical bodies.
func HashSpecSnapshot(snapshotJSON string) string {
	h := sha256.Sum256([]byte(snapshotJSON))
	return fmt.Sprintf("%x", h[:])
}

// BuildSpecSnapshot serialises a resolved definition into a JSON snapshot
// suitable for storage in agent_instances.spec_snapshot.
func BuildSpecSnapshot(def *Definition, provider, model string, tools []string) string {
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
	if def.MaxTurns > 0 {
		snap["max_turns"] = def.MaxTurns
	}
	if def.Timeout > 0 {
		snap["timeout"] = def.Timeout.String()
	}
	scope := ScopeString(def.Scope)
	if scope != "" {
		snap["scope"] = scope
	}
	if def.SourcePath != "" {
		snap["source_path"] = def.SourcePath
	}
	data, _ := json.Marshal(snap)
	return string(data)
}

// SnapshotLimits extracts max_turns/timeout from a spec snapshot JSON
// previously built by BuildSpecSnapshot. Used on resume, where the original
// spec.Definition is not re-resolved from disk - spec identity, including
// its structural limits, comes from the historical snapshot instead (see
// PatchSnapshotResolved). Returns zero values for unparseable input.
func SnapshotLimits(snapshotJSON string) (maxTurns int, timeout time.Duration) {
	var snap struct {
		MaxTurns int    `json:"max_turns"`
		Timeout  string `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &snap); err != nil {
		return 0, 0
	}
	if snap.Timeout != "" {
		timeout, _ = time.ParseDuration(snap.Timeout)
	}
	return snap.MaxTurns, timeout
}

// ScopeString returns the string representation of a skills.Scope.
func ScopeString(scope skills.Scope) string {
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

// ToolsToJSON serialises a tool list as a JSON array string, or "" for nil/empty.
func ToolsToJSON(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	data, _ := json.Marshal(tools)
	return string(data)
}

// ToolsFromJSON parses a tool list previously serialised by ToolsToJSON.
// Empty or unparseable input returns nil (unrestricted) - the same
// nil-means-unrestricted convention used throughout attenuation.
func ToolsFromJSON(s string) []string {
	if s == "" {
		return nil
	}
	var tools []string
	if err := json.Unmarshal([]byte(s), &tools); err != nil {
		return nil
	}
	return tools
}

// PatchSnapshotResolved rewrites the "resolved" provider/model and "tools"
// fields of a spec snapshot JSON, leaving identity fields (name, description,
// body, scope, source_path) untouched. Used on resume: spec identity is
// immutable from the original snapshot (see
// docs/specs/agents/02-spawning-and-lifecycle.md, Model/provider precedence
// on resume), but the resolved model/provider and recomputed effective tools
// for the new instance can differ from the original.
func PatchSnapshotResolved(snapshotJSON, provider, model string, tools []string) (string, error) {
	var snap map[string]any
	if err := json.Unmarshal([]byte(snapshotJSON), &snap); err != nil {
		return "", fmt.Errorf("patch spec snapshot: %w", err)
	}
	snap["resolved"] = map[string]any{
		"provider": provider,
		"model":    model,
	}
	if len(tools) > 0 {
		snap["tools"] = tools
	} else {
		delete(snap, "tools")
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return "", fmt.Errorf("patch spec snapshot: %w", err)
	}
	return string(data), nil
}

// MaxInstanceIDCollisionRetries bounds instance-ID collision retry loops
// around SaveAgentInstance/ResumeSession. 6-char base32 gives ~30 bits of
// entropy, so a collision is already rare; this is a defensive bound per
// docs/specs/agents/04-storage-and-sessions.md (G3/G10: instance ID
// collision retry), not an expected steady-state path.
const MaxInstanceIDCollisionRetries = 5

// MintInstanceID generates a new agent instance address like "research#k3v9qp".
func MintInstanceID(specName string) string {
	return fmt.Sprintf("%s#%s", specName, RandomBase32(6))
}

// RandomBase32 returns n characters of lowercase base32 from crypto/rand.
func RandomBase32(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail on modern systems, but fall back
		// to a time-seeded value rather than panic.
		for i := range b {
			b[i] = alphabet[time.Now().UnixNano()%int64(len(alphabet))]
		}
		return string(b)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
