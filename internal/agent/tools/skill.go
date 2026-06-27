package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/skills"
)

// RegisterSkillTool registers the "Skill" tool into the registry, enabling the
// LLM to look up and activate a named skill from the catalog. When a skill with
// non-empty AllowedTools is activated, the setAllowedTools callback is invoked
// so the coordinator can restrict the available tool set in subsequent turns.
func RegisterSkillTool(reg *Registry, skillsMgr *skills.Manager, tracker *skills.Tracker, setAllowedTools func([]string)) {
	tool := Tool{
		Schema: Schema{
			Name:        "Skill",
			Description: "Activate a skill to get specialised instructions for a task. Call this when a task matches an available skill's description.",
			Parameters:  json.RawMessage(`{"type": "object", "properties": {"name": {"type": "string", "description": "The skill name to activate"}}, "required": ["name"]}`),
		},
		Execute: func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error) {
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
			}

			args.Name = strings.TrimSpace(args.Name)
			if args.Name == "" {
				return Result{Content: "skill name is required", IsError: true}, nil
			}

			snapshot := skillsMgr.Snapshot()

			// Search active skills first, then fall back to all skills so that
			// disabled-but-known skills can still be activated by name.
			matched := findSkill(snapshot.ActiveSkills, args.Name)
			if matched == nil {
				matched = findSkill(snapshot.AllSkills, args.Name)
			}

			if matched == nil {
				return Result{Content: fmt.Sprintf("skill %q not found in catalog", args.Name), IsError: true}, nil
			}

			if tracker != nil {
				tracker.Activate(matched)
			}

			// Notify the coordinator about AllowedTools restrictions (or their
			// absence, which clears any previous filter).
			if setAllowedTools != nil {
				if matched.AllowedTools != "" {
					setAllowedTools(parseAllowedTools(matched.AllowedTools))
				} else {
					setAllowedTools(nil)
				}
			}

			return Result{Content: matched.Instructions}, nil
		},
	}
	_ = reg.Register(tool) // fail only on duplicate (programming error)
}

// findSkill searches a skill slice for one whose name matches (case-insensitive).
func findSkill(skills []*skills.Skill, name string) *skills.Skill {
	lower := strings.ToLower(name)
	for _, s := range skills {
		if s != nil && strings.ToLower(s.Name) == lower {
			return s
		}
	}
	return nil
}

// parseAllowedTools splits a comma/space-separated list of tool names.
func parseAllowedTools(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Normalise commas to spaces, then split on whitespace.
	normalised := strings.ReplaceAll(raw, ",", " ")
	parts := strings.Fields(normalised)
	if len(parts) == 0 {
		return nil
	}
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
