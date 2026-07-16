package agent

import (
	"fmt"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/skills"
)

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
// Definition with SourcePath set - i.e. not one of the embedded built-ins)
// are untrusted relative to built-ins: they may have been authored by the
// model itself rather than reviewed and shipped in the binary. Their
// requested Tools are clamped to the session's current allowlist rather
// than applied as-is, so activating one can never grant a wider tool set
// than the session already had - see clampToolsToCurrentAllowlist.
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
		// Else every requested tool was clamped away - do NOT call
		// SetAllowedTools(clamped) with an empty slice: SetAllowedTools
		// treats an empty list as "no restriction" (its nil-allowedTools
		// sentinel), which would grant everything, the opposite of this
		// guard's purpose. Leave the session's current allowlist as-is,
		// same treatment as the "declared no tools at all" case below.
	case len(def.Tools) > 0:
		c.SetAllowedTools(def.Tools)
	case discovered:
		// A discovered definition with no tools list at all declares no
		// restriction - but for an untrusted definition, treating that as
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
// current tool allowlist. When no mode restriction is active (allowedTools nil)
// but a process ceiling exists (effectiveTools set), the ceiling is used as
// the baseline so the escalation warning fires for tools outside it. Only when
// both are nil is the session truly unrestricted.
func (c *Coordinator) clampToolsToCurrentAllowlist(requested []string) []string {
	c.mu.Lock()
	effective := c.effectiveTools
	allowed := c.allowedTools
	c.mu.Unlock()

	// Determine the baseline: active mode filter takes precedence; if none,
	// fall back to the immutable ceiling. If neither exists, unrestricted.
	baseline := allowed
	if len(baseline) == 0 {
		baseline = effective
	}
	if len(baseline) == 0 {
		return requested
	}

	clamped := make([]string, 0, len(requested))
	for _, name := range requested {
		if baseline[name] {
			clamped = append(clamped, name)
		}
	}
	return clamped
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

	// The notification banner is a single-line widget, not a list view -
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
