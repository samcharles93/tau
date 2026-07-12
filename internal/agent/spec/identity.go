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
// a change to any of them produces a different hash — correctly detecting spec
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
