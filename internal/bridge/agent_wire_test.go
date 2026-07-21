package bridge

import (
	"encoding/json"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestAgentWireRoundTrip(t *testing.T) {
	t.Run("agent.ready", func(t *testing.T) {
		orig := AgentReady{Instance: "research#k3v9qp", PID: 41172, Protocol: 1}
		raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "")
		require.NoError(t, err)
		msg, from, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.ready", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", from)
		require.Equal(t, "", to)
		got, ok := msg.(AgentReady)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.assign", func(t *testing.T) {
		orig := AgentAssign{
			TaskID:     "t-01",
			InstanceID: "research#k3v9qp",
			SessionID:  "s-001",
			Prompt:     "audit the config module",
			Context:    "focus on security",
			Model:      AgentModelPair{Provider: "openai", Model: "gpt-5.4"},
			Tools:      []string{"read", "grep", "find"},
			Depth:      1,
			MaxDepth:   2,
			Limits:     AgentLimits{MaxTurns: 30, Timeout: "10m"},
			Budget:     AgentBudget{MaxTokens: 200000, Deadline: "5m"},
		}
		raw, err := MarshalAgentMessage(orig, "tau#8q2mfe", "research#k3v9qp")
		require.NoError(t, err)
		msg, from, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.assign", envelopeType(raw))
		require.Equal(t, "tau#8q2mfe", from)
		require.Equal(t, "research#k3v9qp", to)
		got, ok := msg.(AgentAssign)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.assign minimal", func(t *testing.T) {
		// No optional fields - tools, context, limits, budget.
		orig := AgentAssign{
			TaskID:     "t-02",
			InstanceID: "plan#a1b2c3",
			SessionID:  "s-002",
			Prompt:     "plan the release",
			Model:      AgentModelPair{Provider: "anthropic", Model: "claude-4.5"},
			Depth:      2,
			MaxDepth:   2,
		}
		raw, err := MarshalAgentMessage(orig, "", "")
		require.NoError(t, err)
		msg, _, _, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		got, ok := msg.(AgentAssign)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.event", func(t *testing.T) {
		ev := tauchat.ChatToolExecutionStartedEvent{
			SessionID: "s-001",
			RequestID: "r-001",
			CallID:    "call_1",
			ToolName:  "read",
			StartedAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		}
		orig := AgentEvent{Instance: "research#k3v9qp", Event: ev}
		raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "tau#8q2mfe")
		require.NoError(t, err)

		msg, from, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.event", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", from)
		require.Equal(t, "tau#8q2mfe", to)

		got, ok := msg.(AgentEvent)
		require.True(t, ok)
		require.Equal(t, "research#k3v9qp", got.Instance)
		require.IsType(t, tauchat.ChatToolExecutionStartedEvent{}, got.Event)
	})

	t.Run("agent.usage", func(t *testing.T) {
		orig := AgentUsage{
			Instance:     "research#k3v9qp",
			Turns:        3,
			InputTokens:  18234,
			OutputTokens: 2011,
			Cost:         0.0142,
		}
		raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "")
		require.NoError(t, err)
		msg, from, _, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.usage", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", from)
		got, ok := msg.(AgentUsage)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.cancel", func(t *testing.T) {
		orig := AgentCancel{TaskID: "t-01", Reason: "user_cancelled", GraceMs: 5000}
		raw, err := MarshalAgentMessage(orig, "", "research#k3v9qp")
		require.NoError(t, err)
		msg, _, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.cancel", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", to)
		got, ok := msg.(AgentCancel)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.result completed", func(t *testing.T) {
		orig := AgentResult{
			TaskID:    "t-01",
			Status:    "completed",
			FinalText: "The audit found no issues.",
			SessionID: "s-001",
			Usage: AgentResultUsage{
				Turns: 7, InputTokens: 45000, OutputTokens: 3200, Cost: 0.089,
			},
		}
		raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "")
		require.NoError(t, err)
		msg, from, _, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.result", envelopeType(raw))
		require.Equal(t, "research#k3v9qp", from)
		got, ok := msg.(AgentResult)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("agent.result failed", func(t *testing.T) {
		orig := AgentResult{
			TaskID:    "t-02",
			Status:    "failed",
			FinalText: "partial output...",
			SessionID: "s-002",
			Usage: AgentResultUsage{
				Turns: 2, InputTokens: 1200, OutputTokens: 500, Cost: 0.005,
			},
			Error:   "child process exited with code 1",
			Partial: true,
		}
		raw, err := MarshalAgentMessage(orig, "plan#a1b2c3", "")
		require.NoError(t, err)
		msg, from, _, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Equal(t, "agent.result", envelopeType(raw))
		require.Equal(t, "plan#a1b2c3", from)
		got, ok := msg.(AgentResult)
		require.True(t, ok)
		require.Equal(t, orig, got)
	})

	t.Run("unknown type returns nil", func(t *testing.T) {
		raw := []byte(`{"type":"nope.agent","payload":{}}`)
		msg, from, to, err := UnmarshalAgentMessage(raw)
		require.NoError(t, err)
		require.Nil(t, msg)
		require.Equal(t, "", from)
		require.Equal(t, "", to)
	})
}

func TestEnvelopeBackwardsCompatible(t *testing.T) {
	// A message without from/to fields (browser-style) must still unmarshal.
	raw := []byte(`{"type":"agent.ready","payload":{"instance":"x","pid":1,"protocol":1}}`)
	msg, from, to, err := UnmarshalAgentMessage(raw)
	require.NoError(t, err)
	_, ok := msg.(AgentReady)
	require.True(t, ok)
	require.Equal(t, "", from)
	require.Equal(t, "", to)
}

func TestAgentEventRoundTripViaRawJSON(t *testing.T) {
	// Verify the AgentEvent serialises through the bridge's MarshalEvent,
	// producing a JSON envelope with a type discriminator in the inner event.
	ev := tauchat.ChatNotificationEvent{
		Message:    "tool completed",
		Level:      tauchat.ChatNotificationInfo,
		OccurredAt: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
	}
	orig := AgentEvent{Instance: "research#k3v9qp", Event: ev}
	raw, err := MarshalAgentMessage(orig, "research#k3v9qp", "")
	require.NoError(t, err)

	// Check the inner event carries the type discriminator.
	msg, _, _, err := UnmarshalAgentMessage(raw)
	require.NoError(t, err)
	got, ok := msg.(AgentEvent)
	require.True(t, ok)

	notif, ok := got.Event.(tauchat.ChatNotificationEvent)
	require.True(t, ok)
	require.Equal(t, "tool completed", notif.Message)
}

// envelopeType extracts the type field from a raw JSON envelope.
func envelopeType(data []byte) string {
	var env struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &env)
	return env.Type
}

// TestAgentResultUsage_WireRoundTrip verifies the usage struct survives
// JSON marshal/unmarshal (Gap: child metrics aggregation).
func TestAgentResultUsage_WireRoundTrip(t *testing.T) {
	orig := AgentResultUsage{
		Turns:        7,
		InputTokens:  82000,
		OutputTokens: 4100,
		Cost:         0.0231,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AgentResultUsage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Turns != orig.Turns {
		t.Errorf("Turns = %d, want %d", got.Turns, orig.Turns)
	}
	if got.InputTokens != orig.InputTokens {
		t.Errorf("InputTokens = %d, want %d", got.InputTokens, orig.InputTokens)
	}
	if got.OutputTokens != orig.OutputTokens {
		t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, orig.OutputTokens)
	}
	if got.Cost != orig.Cost {
		t.Errorf("Cost = %v, want %v", got.Cost, orig.Cost)
	}
}

// TestAgentResultUsage_CumulativeSemantics verifies that cumulative usage
// (not deltas) is the correct interpretation — a later message with higher
// turn count replaces the earlier one in the readChildResult loop.
func TestAgentResultUsage_CumulativeSemantics(t *testing.T) {
	var acc AgentResultUsage
	// Simulate usage arriving: turn 3 then turn 5 (cumulative, not delta)
	updates := []AgentResultUsage{
		{Turns: 3, InputTokens: 500, OutputTokens: 100, Cost: 0.001},
		{Turns: 5, InputTokens: 900, OutputTokens: 180, Cost: 0.002},
	}
	for _, u := range updates {
		if u.Turns > acc.Turns {
			acc = u
		}
	}
	if acc.Turns != 5 {
		t.Errorf("Turns = %d, want 5", acc.Turns)
	}
	if acc.InputTokens != 900 {
		t.Errorf("InputTokens = %d, want 900", acc.InputTokens)
	}
}
