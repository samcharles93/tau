package bridge

import (
	"encoding/json"
	"reflect"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// ---- Agent wire types (docs/specs/agents/03-wire-protocol.md) ----

// AgentReady is the first message a child process writes on stdout
// to announce itself and its protocol version.
type AgentReady struct {
	Instance string `json:"instance"`
	PID      int    `json:"pid"`
	Protocol int    `json:"protocol"`
}

// AgentAssign is the task assignment the parent sends to the child
// after receiving AgentReady.
type AgentAssign struct {
	TaskID     string         `json:"task_id"`
	InstanceID string         `json:"instance_id"`
	SessionID  string         `json:"session_id"`
	Prompt     string         `json:"prompt"`
	Context    string         `json:"context,omitempty"`
	Model      AgentModelPair `json:"model"`
	Tools      []string       `json:"tools,omitempty"`
	Depth      int            `json:"depth"`
	MaxDepth   int            `json:"max_depth"`
	Limits     AgentLimits    `json:"limits"`
	Budget     AgentBudget    `json:"budget"`
}

// AgentModelPair is the resolved provider/model pair sent in an AgentAssign.
// It is always concrete - never a tier.
type AgentModelPair struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// AgentLimits carries structural limits from the spec and config.
type AgentLimits struct {
	MaxTurns int    `json:"max_turns,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
}

// AgentBudget carries per-task budget caps from the spawn call, per
// docs/specs/agents/02-spawning-and-lifecycle.md (Time-based limits:
// timeout and deadline). Timeout is a relative duration ("5m") that
// overrides the spec/config structural timeout (AgentLimits.Timeout) for
// this call only; Deadline is an absolute RFC 3339 timestamp. The two are
// independent - either, both, or neither may be set.
type AgentBudget struct {
	MaxTokens int    `json:"max_tokens,omitempty"`
	Timeout   string `json:"timeout,omitempty"`
	Deadline  string `json:"deadline,omitempty"`
}

// AgentEvent wraps a ChatEvent forwarded from child to parent, tagged
// with the child's instance identity.
type AgentEvent struct {
	Instance string            `json:"instance"`
	Event    tauchat.ChatEvent `json:"event"`
}

// agentEventWire is the wire form of AgentEvent used during marshal/unmarshal
// because ChatEvent is an interface and json.RawMessage is the stable
// serialisation target.
type agentEventWire struct {
	Instance string          `json:"instance"`
	Event    json.RawMessage `json:"event"`
}

// MarshalJSON serialises AgentEvent by delegating to the bridge's event
// marshaler so the inner event carries a type discriminator.
func (ae AgentEvent) MarshalJSON() ([]byte, error) {
	inner, err := MarshalEvent(ae.Event)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentEventWire{
		Instance: ae.Instance,
		Event:    json.RawMessage(inner),
	})
}

// UnmarshalJSON deserialises AgentEvent by extracting the inner envelope
// and looking up the event type in the bridge's event registry.
func (ae *AgentEvent) UnmarshalJSON(data []byte) error {
	var wire agentEventWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	ae.Instance = wire.Instance
	ev, err := unmarshalEventFromEnvelope(wire.Event)
	if err != nil {
		return err
	}
	ae.Event = ev
	return nil
}

var eventTypeByName = func() map[string]reflect.Type {
	m := make(map[string]reflect.Type, len(EventTypes))
	for _, e := range EventTypes {
		m[e.Name] = e.Type
	}
	return m
}()

// unmarshalEventFromEnvelope parses a JSON envelope into a concrete ChatEvent.
func unmarshalEventFromEnvelope(data []byte) (tauchat.ChatEvent, error) {
	var env struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	t, ok := eventTypeByName[env.Type]
	if !ok {
		return nil, nil // unknown event type - not an error, just unsupported
	}
	ptr := reflect.New(t)
	if err := json.Unmarshal(env.Payload, ptr.Interface()); err != nil {
		return nil, err
	}
	ev, ok := ptr.Elem().Interface().(tauchat.ChatEvent)
	if !ok {
		return nil, nil
	}
	return ev, nil
}

// AgentUsage carries cumulative usage totals from child to parent,
// emitted after every completed turn.
type AgentUsage struct {
	Instance     string  `json:"instance"`
	Turns        int     `json:"turns"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// AgentCancel is the parent's signal to stop a running child, with an
// optional grace period before stdin is closed.
type AgentCancel struct {
	TaskID  string `json:"task_id"`
	Reason  string `json:"reason"`
	GraceMs int    `json:"grace_ms,omitempty"`
}

// AgentResult is the terminal message a child writes before exiting.
// The parent must tolerate receiving this with no prior events.
type AgentResult struct {
	TaskID    string           `json:"task_id"`
	Status    string           `json:"status"`
	FinalText string           `json:"final_text"`
	SessionID string           `json:"session_id"`
	Usage     AgentResultUsage `json:"usage"`
	Error     string           `json:"error,omitempty"`
	Partial   bool             `json:"partial"`
}

// AgentResultUsage carries the child's final usage totals.
type AgentResultUsage struct {
	Turns        int     `json:"turns"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// AgentMessageTypeInfo pairs a wire discriminator with the concrete Go type
// used for the agent wire protocol. tools/specgen reads this registry to
// (re)generate docs/asyncapi/tau.yaml.
type AgentMessageTypeInfo struct {
	Name string
	Type reflect.Type
}

// AgentMessageTypes is the registry of agent-wire message types.
var AgentMessageTypes = []AgentMessageTypeInfo{
	{"agent.ready", reflect.TypeFor[AgentReady]()},
	{"agent.assign", reflect.TypeFor[AgentAssign]()},
	{"agent.event", reflect.TypeFor[AgentEvent]()},
	{"agent.usage", reflect.TypeFor[AgentUsage]()},
	{"agent.cancel", reflect.TypeFor[AgentCancel]()},
	{"agent.result", reflect.TypeFor[AgentResult]()},
}

var agentMessageTypeByName = func() map[string]reflect.Type {
	m := make(map[string]reflect.Type, len(AgentMessageTypes))
	for _, t := range AgentMessageTypes {
		m[t.Name] = t.Type
	}
	return m
}()

// MarshalAgentMessage serialises an agent wire message into a JSON envelope
// with type discriminator and optional from/to fields.
func MarshalAgentMessage(msg any, from, to string) ([]byte, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{
		Type:    agentMessageTypeName(msg),
		From:    from,
		To:      to,
		Payload: body,
	})
}

// UnmarshalAgentMessage parses a JSON envelope into a concrete agent wire
// message type.
func UnmarshalAgentMessage(data []byte) (msg any, from, to string, err error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, "", "", err
	}
	t, ok := agentMessageTypeByName[env.Type]
	if !ok {
		return nil, env.From, env.To, nil // unknown type, not an agent message
	}
	ptr := reflect.New(t)
	if err := json.Unmarshal(env.Payload, ptr.Interface()); err != nil {
		return nil, env.From, env.To, err
	}
	return ptr.Elem().Interface(), env.From, env.To, nil
}

func agentMessageTypeName(msg any) string {
	t := reflect.TypeOf(msg)
	for _, info := range AgentMessageTypes {
		if info.Type == t {
			return info.Name
		}
	}
	return "unknown.agent"
}
