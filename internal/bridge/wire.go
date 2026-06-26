// Package bridge provides a WebSocket gateway between browser clients and the
// Tau chat runtime. The wire protocol wraps every chat.ChatEvent and
// chat.ChatCommand in a JSON envelope with a discriminator field so TypeScript
// consumers and Go can unmarshal interface-typed values.
package bridge

import (
	"encoding/json"
	"fmt"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// Envelope is the on-the-wire wrapper for every WebSocket message.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// EventEnvelope carries a chat.ChatEvent to the browser.
type EventEnvelope struct {
	Type    string            `json:"type"`
	Payload tauchat.ChatEvent `json:"payload"`
}

// InitMessage is sent once on connection so the SPA can render initial state.
type InitMessage struct {
	Type      string               `json:"type"`
	SessionID string               `json:"session_id"`
	Model     string               `json:"model"`
	Provider  string               `json:"provider"`
	Commands  []tauchat.CommandRef `json:"commands,omitempty"`
}

// MarshalEvent serializes a ChatEvent into an envelope with a type discriminator.
func MarshalEvent(ev tauchat.ChatEvent) ([]byte, error) {
	body, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	return json.Marshal(Envelope{
		Type:    eventTypeName(ev),
		Payload: body,
	})
}

// UnmarshalCommand parses a JSON envelope into a concrete ChatCommand.
func UnmarshalCommand(data []byte) (tauchat.ChatCommand, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal command envelope: %w", err)
	}
	return unmarshalCommandPayload(env.Type, env.Payload)
}

// MarshalInit builds the one-time init message.
func MarshalInit(sessionID, model, provider string, commands []tauchat.CommandRef) ([]byte, error) {
	return json.Marshal(InitMessage{
		Type:      "init",
		SessionID: sessionID,
		Model:     model,
		Provider:  provider,
		Commands:  commands,
	})
}

func eventTypeName(ev tauchat.ChatEvent) string {
	switch ev.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		return "ChatSessionSnapshotEvent"
	case tauchat.ChatResponseStartedEvent:
		return "ChatResponseStartedEvent"
	case tauchat.ChatResponseDeltaEvent:
		return "ChatResponseDeltaEvent"
	case tauchat.ChatReasoningDeltaEvent:
		return "ChatReasoningDeltaEvent"
	case tauchat.ChatToolCallDeltaEvent:
		return "ChatToolCallDeltaEvent"
	case tauchat.ChatToolExecutionStartedEvent:
		return "ChatToolExecutionStartedEvent"
	case tauchat.ChatToolExecutionCompletedEvent:
		return "ChatToolExecutionCompletedEvent"
	case tauchat.ChatToolOutputEvent:
		return "ChatToolOutputEvent"
	case tauchat.ChatResponseCompletedEvent:
		return "ChatResponseCompletedEvent"
	case tauchat.ChatResponseCancelledEvent:
		return "ChatResponseCancelledEvent"
	case tauchat.ChatRuntimeErrorEvent:
		return "ChatRuntimeErrorEvent"
	case tauchat.ChatNotificationEvent:
		return "ChatNotificationEvent"
	case tauchat.ExtensionsReloadedEvent:
		return "ExtensionsReloadedEvent"
	case tauchat.ExtensionCommandsChangedEvent:
		return "ExtensionCommandsChangedEvent"
	case tauchat.ExtensionCommandResultEvent:
		return "ExtensionCommandResultEvent"
	case tauchat.InteractivePromptRequestedEvent:
		return "InteractivePromptRequestedEvent"
	case tauchat.SessionsListedEvent:
		return "SessionsListedEvent"
	case tauchat.SessionLoadedEvent:
		return "SessionLoadedEvent"
	case tauchat.SessionDeletedEvent:
		return "SessionDeletedEvent"
	case tauchat.SessionExportedEvent:
		return "SessionExportedEvent"
	case tauchat.CommandsChangedEvent:
		return "CommandsChangedEvent"
	default:
		return "UnknownChatEvent"
	}
}

func unmarshalCommandPayload(name string, raw json.RawMessage) (tauchat.ChatCommand, error) {
	var cmd tauchat.ChatCommand
	switch name {
	case "StartChatSessionCommand":
		var v tauchat.StartChatSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "SubmitChatPromptCommand":
		var v tauchat.SubmitChatPromptCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "SteerChatPromptCommand":
		var v tauchat.SteerChatPromptCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "UpdateChatSessionCommand":
		var v tauchat.UpdateChatSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "CancelChatRequestCommand":
		var v tauchat.CancelChatRequestCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "ResetChatSessionCommand":
		var v tauchat.ResetChatSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "CloseChatSessionCommand":
		var v tauchat.CloseChatSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "ReloadExtensionsCommand":
		var v tauchat.ReloadExtensionsCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "RunExtensionCommandCommand":
		var v tauchat.RunExtensionCommandCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "RespondInteractivePromptCommand":
		var v tauchat.RespondInteractivePromptCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "ListSessionsCommand":
		var v tauchat.ListSessionsCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "LoadSessionCommand":
		var v tauchat.LoadSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "DeleteSessionCommand":
		var v tauchat.DeleteSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	case "ExportSessionCommand":
		var v tauchat.ExportSessionCommand
		err := json.Unmarshal(raw, &v)
		cmd = v
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown command type %q", name)
	}
	return cmd, nil
}
