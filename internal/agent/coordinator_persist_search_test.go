package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
)

func TestHandleSearchSessions_NoSessionManager_EmitsWarning(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, nil)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "anything"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	notif := event.(chat.ChatNotificationEvent)
	require.Equal(t, chat.ChatNotificationWarn, notif.Level)
	require.Contains(t, notif.Message, "not available")
}

func TestHandleSearchSessions_EmptyQuery_EmitsUsage(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, newTestSessionManager(t))
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "   "}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatNotificationEvent)
		return ok
	})
	require.Contains(t, event.(chat.ChatNotificationEvent).Message, "/search")
}

func TestHandleSearchSessions_ReturnsHits(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "search-sess-1", "how do I rotate the kubeconfig certificate")
	persistSessionWithMessage(t, coordinator, sub, "search-sess-2", "favorite pasta recipe")

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "kubeconfig", Limit: 10}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SearchResultsEvent)
		return ok
	})
	res := event.(chat.SearchResultsEvent)
	require.Equal(t, "kubeconfig", res.Query)
	require.Len(t, res.Hits, 1)
	require.Equal(t, "search-sess-1", res.Hits[0].SessionID)
	require.Equal(t, "user", res.Hits[0].Role)
	require.Contains(t, res.Hits[0].Snippet, "**kubeconfig**")
	require.False(t, res.Hits[0].CreatedAt.IsZero())
}

func TestHandleSearchSessions_PaginationCursor(t *testing.T) {
	mgr := newTestSessionManager(t)
	coordinator := newTestCoordinatorWithManager(t, mgr)
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	persistSessionWithMessage(t, coordinator, sub, "pg-1", "shared searchterm here")
	persistSessionWithMessage(t, coordinator, sub, "pg-2", "shared searchterm there")

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "searchterm", Limit: 1}))
	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.SearchResultsEvent)
		return ok
	})
	page1 := event.(chat.SearchResultsEvent)
	require.Len(t, page1.Hits, 1)
	require.NotEmpty(t, page1.NextCursor, "a full page sets a next cursor")

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "searchterm", Limit: 1, Cursor: page1.NextCursor}))
	event = drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		r, ok := e.(chat.SearchResultsEvent)
		return ok && r.NextCursor == ""
	})
	page2 := event.(chat.SearchResultsEvent)
	require.Len(t, page2.Hits, 1)
	require.NotEqual(t, page1.Hits[0].SessionID, page2.Hits[0].SessionID, "cursor pages are disjoint")
}

func TestHandleSearchSessions_InvalidCursor_EmitsError(t *testing.T) {
	coordinator := newTestCoordinatorWithManager(t, newTestSessionManager(t))
	sub, err := coordinator.SubscribeEvents()
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, coordinator.Send(chat.SearchSessionsCommand{Query: "x", Cursor: "not-a-number"}))

	event := drainUntil(t, sub, time.Second, func(e chat.ChatEvent) bool {
		_, ok := e.(chat.ChatRuntimeErrorEvent)
		return ok
	})
	require.Contains(t, event.(chat.ChatRuntimeErrorEvent).Message, "cursor")
}
