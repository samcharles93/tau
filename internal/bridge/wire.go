// Package bridge provides a WebSocket gateway between browser clients and the
// Tau chat runtime. The wire protocol wraps every chat.ChatEvent and
// chat.ChatCommand in a JSON envelope with a discriminator field so TypeScript
// consumers and Go can unmarshal interface-typed values.
package bridge

import (
	"encoding/json"
	"fmt"
	"reflect"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// Envelope is the on-the-wire wrapper for every WebSocket message.
// From/To are optional agent-instance addresses used by the agent wire
// protocol (see docs/specs/agents/03-wire-protocol.md). Browser-bound
// messages omit these fields, preserving backwards compatibility.
type Envelope struct {
	Type    string          `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// EventEnvelope carries a chat.ChatEvent to the browser.
type EventEnvelope struct {
	Type    string            `json:"type"`
	Payload tauchat.ChatEvent `json:"payload"`
}

// InitMessage is sent once on connection so the SPA can render initial state.
type InitMessage struct {
	Type              string                     `json:"type"`
	SessionID         string                     `json:"session_id"`
	Model             string                     `json:"model"`
	Provider          string                     `json:"provider"`
	Models            []tauchat.ChatModelRef     `json:"models,omitempty"`
	Providers         []string                   `json:"providers,omitempty"`
	Commands          []tauchat.CommandRef       `json:"commands,omitempty"`
	Skills            []tauchat.SkillInfo        `json:"skills,omitempty"`
	ExtensionCommands []tauchat.ExtensionCommand `json:"extension_commands,omitempty"`
}

// EventTypeInfo pairs a wire discriminator name with its concrete
// chat.ChatEvent type.
type EventTypeInfo struct {
	Name string
	Type reflect.Type
}

// EventTypes is the authoritative registry of ChatEvent types the bridge
// puts on the wire. Anything not listed here never reaches browser clients.
// tools/specgen reads this registry to (re)generate docs/asyncapi/tau.yaml,
// so adding an event here is what puts it in the published spec.
var EventTypes = []EventTypeInfo{
	{"ChatSessionSnapshotEvent", reflect.TypeFor[tauchat.ChatSessionSnapshotEvent]()},
	{"ChatResponseStartedEvent", reflect.TypeFor[tauchat.ChatResponseStartedEvent]()},
	{"ChatResponseDeltaEvent", reflect.TypeFor[tauchat.ChatResponseDeltaEvent]()},
	{"ChatReasoningDeltaEvent", reflect.TypeFor[tauchat.ChatReasoningDeltaEvent]()},
	{"ChatToolCallDeltaEvent", reflect.TypeFor[tauchat.ChatToolCallDeltaEvent]()},
	{"ChatToolExecutionStartedEvent", reflect.TypeFor[tauchat.ChatToolExecutionStartedEvent]()},
	{"ChatToolExecutionCompletedEvent", reflect.TypeFor[tauchat.ChatToolExecutionCompletedEvent]()},
	{"ChatToolOutputEvent", reflect.TypeFor[tauchat.ChatToolOutputEvent]()},
	{"ChatResponseCompletedEvent", reflect.TypeFor[tauchat.ChatResponseCompletedEvent]()},
	{"ChatResponseCancelledEvent", reflect.TypeFor[tauchat.ChatResponseCancelledEvent]()},
	{"ChatRuntimeErrorEvent", reflect.TypeFor[tauchat.ChatRuntimeErrorEvent]()},
	{"ChatNotificationEvent", reflect.TypeFor[tauchat.ChatNotificationEvent]()},
	{"ExtensionsReloadedEvent", reflect.TypeFor[tauchat.ExtensionsReloadedEvent]()},
	{"ExtensionCommandsChangedEvent", reflect.TypeFor[tauchat.ExtensionCommandsChangedEvent]()},
	{"ExtensionCommandResultEvent", reflect.TypeFor[tauchat.ExtensionCommandResultEvent]()},
	{"ExtensionViewRenderedEvent", reflect.TypeFor[tauchat.ExtensionViewRenderedEvent]()},
	{"ExtensionViewClosedEvent", reflect.TypeFor[tauchat.ExtensionViewClosedEvent]()},
	{"InteractivePromptRequestedEvent", reflect.TypeFor[tauchat.InteractivePromptRequestedEvent]()},
	{"SessionsListedEvent", reflect.TypeFor[tauchat.SessionsListedEvent]()},
	{"SessionLoadedEvent", reflect.TypeFor[tauchat.SessionLoadedEvent]()},
	{"SessionDeletedEvent", reflect.TypeFor[tauchat.SessionDeletedEvent]()},
	{"SessionExportedEvent", reflect.TypeFor[tauchat.SessionExportedEvent]()},
	{"CommandsChangedEvent", reflect.TypeFor[tauchat.CommandsChangedEvent]()},
	{"SkillsChangedEvent", reflect.TypeFor[tauchat.SkillsChangedEvent]()},
}

// CommandTypeInfo pairs a wire discriminator name with its concrete
// chat.ChatCommand type.
type CommandTypeInfo struct {
	Name string
	Type reflect.Type
}

// CommandTypes is the authoritative registry of ChatCommand types the bridge
// accepts from browser clients. See EventTypes.
var CommandTypes = []CommandTypeInfo{
	{"StartChatSessionCommand", reflect.TypeFor[tauchat.StartChatSessionCommand]()},
	{"SubmitChatPromptCommand", reflect.TypeFor[tauchat.SubmitChatPromptCommand]()},
	{"SteerChatPromptCommand", reflect.TypeFor[tauchat.SteerChatPromptCommand]()},
	{"UpdateChatSessionCommand", reflect.TypeFor[tauchat.UpdateChatSessionCommand]()},
	{"CancelChatRequestCommand", reflect.TypeFor[tauchat.CancelChatRequestCommand]()},
	{"ResetChatSessionCommand", reflect.TypeFor[tauchat.ResetChatSessionCommand]()},
	{"CloseChatSessionCommand", reflect.TypeFor[tauchat.CloseChatSessionCommand]()},
	{"ReloadExtensionsCommand", reflect.TypeFor[tauchat.ReloadExtensionsCommand]()},
	{"RunExtensionCommandCommand", reflect.TypeFor[tauchat.RunExtensionCommandCommand]()},
	{"RespondInteractivePromptCommand", reflect.TypeFor[tauchat.RespondInteractivePromptCommand]()},
	{"ListSessionsCommand", reflect.TypeFor[tauchat.ListSessionsCommand]()},
	{"LoadSessionCommand", reflect.TypeFor[tauchat.LoadSessionCommand]()},
	{"DeleteSessionCommand", reflect.TypeFor[tauchat.DeleteSessionCommand]()},
	{"ExportSessionCommand", reflect.TypeFor[tauchat.ExportSessionCommand]()},
	{"RunSkillCommand", reflect.TypeFor[tauchat.RunSkillCommand]()},
	{"RunAgentCommand", reflect.TypeFor[tauchat.RunAgentCommand]()},
	{"ReloadSkillsCommand", reflect.TypeFor[tauchat.ReloadSkillsCommand]()},
	{"ListSkillsCommand", reflect.TypeFor[tauchat.ListSkillsCommand]()},
}

var eventNameByType = func() map[reflect.Type]string {
	m := make(map[reflect.Type]string, len(EventTypes))
	for _, e := range EventTypes {
		m[e.Type] = e.Name
	}
	return m
}()

var commandTypeByName = func() map[string]reflect.Type {
	m := make(map[string]reflect.Type, len(CommandTypes))
	for _, c := range CommandTypes {
		m[c.Name] = c.Type
	}
	return m
}()

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
func MarshalInit(sessionID, model, provider string, models []tauchat.ChatModelRef, providers []string, commands []tauchat.CommandRef, skills []tauchat.SkillInfo, extCmds []tauchat.ExtensionCommand) ([]byte, error) {
	return json.Marshal(InitMessage{
		Type:              "init",
		SessionID:         sessionID,
		Model:             model,
		Provider:          provider,
		Models:            models,
		Providers:         providers,
		Commands:          commands,
		Skills:            skills,
		ExtensionCommands: extCmds,
	})
}

func eventTypeName(ev tauchat.ChatEvent) string {
	if name, ok := eventNameByType[reflect.TypeOf(ev)]; ok {
		return name
	}
	return "UnknownChatEvent"
}

func unmarshalCommandPayload(name string, raw json.RawMessage) (tauchat.ChatCommand, error) {
	t, ok := commandTypeByName[name]
	if !ok {
		return nil, fmt.Errorf("unknown command type %q", name)
	}
	ptr := reflect.New(t)
	if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
		return nil, err
	}
	cmd, ok := ptr.Elem().Interface().(tauchat.ChatCommand)
	if !ok {
		return nil, fmt.Errorf("command type %q does not implement ChatCommand", name)
	}
	return cmd, nil
}
