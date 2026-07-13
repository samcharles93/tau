package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/sessions"
)

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
		c.loggerWith(snapshot.SessionID).Error(
			"saving default provider/model to local config",
			"err", err,
		)
	}
}

func (c *Coordinator) handleListSessions(cmd chat.ListSessionsCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Session persistence is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
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
		Silent:     cmd.Silent,
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

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
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

// handleLoadChildTranscript loads a finished child agent's session for
// read-only drill-down. Unlike handleLoadSession, it never touches
// c.sessions — the child is never the runtime's active session.
func (c *Coordinator) handleLoadChildTranscript(cmd chat.LoadChildTranscriptCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	loaded, err := c.sessionManager.Load(ctx, cmd.SessionID, nil)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    fmt.Sprintf("loading child transcript: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	c.emit(chat.ChildTranscriptLoadedEvent{
		SessionID: loaded.SessionID,
		Messages:  loaded.Messages,
	})
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

	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
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
		ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
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
		ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
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
	if c.sessionManager == nil || c.noPersist {
		return
	}
	// Nothing was ever said — persisting an empty row just clutters the
	// session list (/session, --resume) with entries that can never be
	// resumed into anything.
	if len(state.Messages) == 0 {
		return
	}

	// Deliberately not c.ctx: this runs from cancelAllSessions on the
	// c.ctx.Done() shutdown path, so a context derived from c.ctx would
	// already be cancelled and the final save would never happen.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.sessionManager.Save(ctx, state, duration); err != nil {
		c.loggerWith(state.SessionID).Error(
			"persist session failed",
			"err", err,
		)
		return
	}

	c.loggerWith(state.SessionID).Debug(
		"session persisted",
		"msg_count", len(state.Messages),
		"duration_ms", duration.Milliseconds(),
	)

	if !c.autoExportJSONL {
		return
	}

	// Auto-export JSONL as a background convenience artifact.
	go func() {
		exportPath := c.sessionManager.SessionJSONLPath(state.SessionID, state.CreatedAt)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.sessionManager.ExportToJSONL(ctx, state.SessionID, exportPath); err != nil {
			c.loggerWith(state.SessionID).Warn(
				"auto-export jsonl failed",
				"err", err,
			)
		}
	}()
}
