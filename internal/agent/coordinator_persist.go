package agent

import (
	"context"
	"fmt"
	"strconv"
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

func (c *Coordinator) handleListSessions(ctx context.Context, cmd chat.ListSessionsCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Session persistence is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

// searchDefaultLimit matches the store's default page size.
const searchDefaultLimit = 20

// handleSearchSessions runs a full-text search over saved session messages.
// The cursor is an opaque decimal hit offset, mirroring List's pagination.
func (c *Coordinator) handleSearchSessions(ctx context.Context, cmd chat.SearchSessionsCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Session persistence is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}
	if strings.TrimSpace(cmd.Query) == "" {
		c.emit(chat.ChatNotificationEvent{
			Message:    "usage: /search <query>",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}
	offset, err := strconv.Atoi(cmd.Cursor)
	if cmd.Cursor != "" && err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("invalid search cursor %q", cmd.Cursor),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}
	limit := cmd.Limit
	if limit <= 0 {
		limit = searchDefaultLimit
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Fetch one extra row to detect a next page, mirroring List.
	hits, err := c.sessionManager.Search(ctx, cmd.Query, limit+1, offset)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    fmt.Sprintf("searching sessions: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	nextCursor := ""
	if len(hits) > limit {
		hits = hits[:limit]
		nextCursor = strconv.Itoa(offset + limit)
	}
	c.emit(chat.SearchResultsEvent{
		Query:      cmd.Query,
		Hits:       hits,
		NextCursor: nextCursor,
	})
}

func (c *Coordinator) handleLoadSession(ctx context.Context, cmd chat.LoadSessionCommand) {
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

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
// c.sessions - the child is never the runtime's active session.
func (c *Coordinator) handleLoadChildTranscript(ctx context.Context, cmd chat.LoadChildTranscriptCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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

func (c *Coordinator) handleDeleteSession(ctx context.Context, cmd chat.DeleteSessionCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

func (c *Coordinator) handleExportSession(ctx context.Context, cmd chat.ExportSessionCommand) {
	if c.sessionManager == nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Session persistence is not available",
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	format := chat.NormalizeExportFormat(cmd.Format)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// With no output file the rendered payload is collected for the event;
	// otherwise it is written atomically to cmd.Output.
	content, err := c.sessionManager.Export(ctx, cmd.SessionID, format, cmd.Output)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    fmt.Sprintf("exporting session: %v", err),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	// The export payload travels on the event, never on os.Stdout: writing to
	// stdout from here corrupts the rendered frame during interactive TUI
	// sessions. Frontends (and headless renderers) present Content themselves.
	c.emit(chat.SessionExportedEvent{
		SessionID: cmd.SessionID,
		Format:    format,
		Path:      cmd.Output,
		Content:   content,
	})
}

// persistSession saves the session state to the store. It is called on graceful
// close and on forced shutdown. Errors are logged but not surfaced to the TUI -
// persistence is best-effort.
func (c *Coordinator) persistSession(ctx context.Context, state chat.ChatSessionState, duration time.Duration) {
	if c.sessionManager == nil || c.noPersist {
		return
	}
	// Nothing was ever said - persisting an empty row just clutters the
	// session list (/session, --resume) with entries that can never be
	// resumed into anything.
	if len(state.Messages) == 0 {
		return
	}

	// Runs from cancelAllSessions on the shutdown path, where the passed-in
	// ctx is already cancelled - detach cancellation so the final save can
	// still happen (WithoutCancel keeps values, drops the Done channel).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
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

	// Auto-export JSONL as a background convenience artifact. Tracked in
	// exportWG so the shutdown path can wait for it before Close() returns
	// and the caller closes the session store (tau-eub).
	c.exportWG.Go(func() {
		exportPath := c.sessionManager.SessionJSONLPath(state.SessionID, state.CreatedAt)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := c.sessionManager.ExportToJSONL(ctx, state.SessionID, exportPath); err != nil {
			c.loggerWith(state.SessionID).Warn(
				"auto-export jsonl failed",
				"err", err,
			)
		}
	})
}
