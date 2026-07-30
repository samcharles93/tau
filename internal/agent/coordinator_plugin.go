package agent

import (
	"encoding/json"
	"sort"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// marshalMessages serializes messages for plugin event payloads.
func marshalMessages(msgs []chat.ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		b, _ := json.Marshal(m)
		out[i] = string(b)
	}
	return out
}

// marshalParameters serializes chat parameters for plugin event payloads.
func marshalParameters(p chat.ChatParameters) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// applyPluginMessageModifications applies message injections and removals from a
// plugin EventResponse to the provided session state. It processes removals in
// descending index order to avoid index shifting, then appends injected messages.
// Malformed injected messages are skipped rather than failing the turn.
func (c *Coordinator) applyPluginMessageModifications(state *chat.ChatSessionState, resp *api.EventResponse) {
	if resp == nil || state == nil {
		return
	}
	// Process removals in descending order to keep indices stable.
	indices := make([]int32, len(resp.GetRemoveMessageIndices()))
	copy(indices, resp.GetRemoveMessageIndices())
	sort.Slice(indices, func(i, j int) bool { return indices[i] > indices[j] })
	for _, idx := range indices {
		if int(idx) >= 0 && int(idx) < len(state.Messages) {
			state.Messages = append(state.Messages[:idx], state.Messages[idx+1:]...)
		}
	}
	// Inject messages.
	for _, raw := range resp.GetInjectMessages() {
		var msg chat.ChatMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			c.loggerWith(state.SessionID).Warn(
				"failed to decode injected message",
				"err", err,
			)
			continue
		}
		state.Messages = append(state.Messages, msg)
	}
}

// dispatchPluginRequestResponse dispatches a request-response lifecycle
// event to plugins via the configured callback. Use publishPluginLifecycleEvent
// for fire-and-forget notifications that don't need a response.
func (c *Coordinator) dispatchPluginRequestResponse(event, sessionID string, payload *api.EventPayload) *api.EventResponse {
	if c.onPluginEvent == nil {
		return nil
	}
	return c.onPluginEvent(event, sessionID, payload)
}

// publishPluginLifecycleEvent publishes a fire-and-forget lifecycle
// notification on the event bus. Plugin manager (or any subscriber)
// receives these asynchronously.
func (c *Coordinator) publishPluginLifecycleEvent(event, sessionID string, payload *api.EventPayload) {
	c.loggerWith(sessionID).Debug(
		"plugin lifecycle event emitted",
		"event", event,
		"payload_kind", pluginPayloadKind(payload),
	)
	c.pluginPub.Publish(chat.PluginLifecycleEvent{
		Event:     event,
		SessionID: sessionID,
		Payload:   payload,
	})
}
