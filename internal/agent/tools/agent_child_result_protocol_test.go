package tools

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// Protocol-compliance tests for readChildResult, pinning the wire-protocol
// frame rules from docs/specs/agents/03-wire-protocol.md: authority
// validation (instance mismatch = violation, cap maxAuthorityViolations
// closes the pipe), duplicate agent.result discarded, post-result messages
// rejected, and EOF-without-result synthesis (cancelled vs failed by ctx).

const protocolTestInstance = "research#1"

// wireMsg is one envelope on the child's stdout.
type wireMsg struct {
	typ     string
	payload any
}

func childEvent(instance string) wireMsg {
	return wireMsg{"agent.event", map[string]any{
		"instance": instance,
		"event": map[string]any{
			"type": "ChatResponseStartedEvent",
			"payload": map[string]any{
				"session_id": "child-session",
				"request_id": "req-1",
				"started_at": time.Now(),
			},
		},
	}}
}

func childUsage(instance string, turns int) wireMsg {
	return wireMsg{"agent.usage", map[string]any{
		"instance":      instance,
		"turns":         turns,
		"input_tokens":  100 * turns,
		"output_tokens": 10 * turns,
		"cost":          float64(turns) / 100,
	}}
}

func childResult(status, finalText string) wireMsg {
	return wireMsg{"agent.result", map[string]any{
		"task_id":    protocolTestInstance,
		"status":     status,
		"final_text": finalText,
	}}
}

func unknownType(typ string) wireMsg {
	return wireMsg{typ, map[string]any{"instance": protocolTestInstance}}
}

// feedChild writes msgs as JSONL envelopes into a buffer and returns a
// stdio.Reader over it, positioned exactly as the parent's reader is after
// the handshake.
func feedChild(t *testing.T, msgs []wireMsg) *stdio.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := stdio.NewWriter(&buf)
	for _, m := range msgs {
		require.NoError(t, w.WriteEnvelope(m.typ, m.payload))
	}
	return stdio.NewReader(&buf)
}

// runChildResult drains msgs through readChildResult with a nil publisher:
// publishChildEvent and emitState both no-op on nil, so these tests exercise
// pure wire-protocol handling without a bus.
func runChildResult(t *testing.T, ctx context.Context, msgs []wireMsg) (string, *bridge.AgentResult, *bridge.AgentResultUsage, error) {
	t.Helper()
	return readChildResult(ctx, feedChild(t, msgs), protocolTestInstance, "call-1", "parent-session", nil)
}

func TestReadChildResult_ProtocolCompliance(t *testing.T) {
	const inst = protocolTestInstance
	tests := []struct {
		name               string
		msgs               []wireMsg
		wantErr            string // substring of the returned error; "" means nil error
		wantErrNotContains string
		wantStatus         string // expected result.Status when wantErr == ""
		wantFinalText      string
		wantUsageTurns     int
	}{
		{
			name:          "single result terminates the stream",
			msgs:          []wireMsg{childResult("completed", "done")},
			wantStatus:    "completed",
			wantFinalText: "done",
		},
		{
			// Per the spec's frame rules the pipe is closed after the
			// first agent.result, so the second one is never dispatched:
			// silently discarded, not fatal, first result wins.
			name:          "duplicate agent.result silently discarded",
			msgs:          []wireMsg{childResult("completed", "first"), childResult("failed", "second")},
			wantStatus:    "completed",
			wantFinalText: "first",
		},
		{
			// Same for any message after the terminal one: the reader has
			// stopped at agent.result, so post-result frames are rejected
			// by virtue of the pipe closing - they can neither error nor
			// mutate the returned result.
			name:          "post-result messages rejected",
			msgs:          []wireMsg{childResult("completed", "done"), unknownType("agent.bogus"), childEvent("evil#2")},
			wantStatus:    "completed",
			wantFinalText: "done",
		},
		{
			name: "events and usage stream before result, highest cumulative usage wins",
			msgs: []wireMsg{
				childEvent(inst),
				childUsage(inst, 2),
				childUsage(inst, 5),
				childResult("completed", "done"),
			},
			wantStatus:     "completed",
			wantFinalText:  "done",
			wantUsageTurns: 5,
		},
		{
			name: "stale lower usage does not roll the accumulator back",
			msgs: []wireMsg{
				childUsage(inst, 5),
				childUsage(inst, 2),
				childResult("completed", "done"),
			},
			wantStatus:     "completed",
			wantFinalText:  "done",
			wantUsageTurns: 5,
		},
		{
			name: "instance mismatch below the cap is tolerated",
			msgs: []wireMsg{
				childEvent("evil#2"),
				childEvent("evil#2"),
				childResult("completed", "done"),
			},
			wantStatus:    "completed",
			wantFinalText: "done",
		},
		{
			name: "instance mismatch at the cap closes the pipe",
			msgs: []wireMsg{
				childEvent("evil#2"),
				childEvent("evil#2"),
				childEvent("evil#2"),
			},
			wantErr: "protocol violation: 3 authority violations, closing pipe",
		},
		{
			name: "unknown message types at the cap close the pipe",
			msgs: []wireMsg{
				unknownType("agent.bogus"),
				unknownType("agent.bogus"),
				unknownType("agent.bogus"),
			},
			wantErr: `protocol violation: 3 authority violations (unknown type "agent.bogus"), closing pipe`,
		},
		{
			name: "mixed violation kinds accumulate toward the cap",
			msgs: []wireMsg{
				childEvent("evil#2"),       // mismatch
				unknownType("agent.bogus"), // unknown type
				childUsage("evil#2", 1),    // mismatch - third violation, no unknown type in the message
			},
			wantErr:            "protocol violation: 3 authority violations, closing pipe",
			wantErrNotContains: "unknown type",
		},
		{
			name: "malformed usage envelope is skipped, not fatal",
			msgs: []wireMsg{
				{"agent.usage", map[string]any{"instance": inst, "turns": "lots"}},
				childResult("completed", "done"),
			},
			wantStatus:     "completed",
			wantFinalText:  "done",
			wantUsageTurns: 0,
		},
		{
			name:    "malformed result payload is a hard error",
			msgs:    []wireMsg{{"agent.result", map[string]any{"task_id": inst, "status": 42}}},
			wantErr: "unmarshal agent.result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res, usage, err := runChildResult(t, context.Background(), tt.msgs)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				if tt.wantErrNotContains != "" {
					require.NotContains(t, err.Error(), tt.wantErrNotContains)
				}
				require.Nil(t, res, "a violation error must not carry a result")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tt.wantStatus, res.Status)
			require.Equal(t, tt.wantFinalText, res.FinalText)
			require.NotNil(t, usage)
			require.Equal(t, tt.wantUsageTurns, usage.Turns)
		})
	}
}

// TestReadChildResult_EOFWithoutResult pins the synthesis contract from
// 02-spawning-and-lifecycle.md Phase 3: stdout EOF without agent.result
// means the child died (synthesise "failed") unless the parent's own context
// was cancelled (synthesise "cancelled"); synthesis is not an error return.
func TestReadChildResult_EOFWithoutResult(t *testing.T) {
	t.Run("live context synthesizes failed with partial usage", func(t *testing.T) {
		_, res, usage, err := runChildResult(t, context.Background(),
			[]wireMsg{childEvent(protocolTestInstance), childUsage(protocolTestInstance, 3)})
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, string(tauchat.ChildAgentFailed), res.Status)
		require.True(t, res.Partial)
		require.Contains(t, res.Error, "child exited without result")
		require.Equal(t, protocolTestInstance, res.TaskID)
		require.Equal(t, 3, res.Usage.Turns, "synthesised result carries the accumulated usage")
		require.Equal(t, 3, usage.Turns)
	})

	t.Run("cancelled context synthesizes cancelled without error detail", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, res, _, err := runChildResult(t, ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, string(tauchat.ChildAgentCancelled), res.Status)
		require.True(t, res.Partial)
		require.Empty(t, res.Error)
	})
}

// collectChildEvents wires a real bus: returns the publisher to hand to
// readChildResult plus a stop function that drains and returns everything
// published. Publish blocks until a subscriber receives, so the collector
// goroutine must run for the lifetime of the read (same pattern as
// TestReadChildResult_DoesNotLeakRawWirePayloadAsToolOutput).
func collectChildEvents(t *testing.T) (*eventbus.Publisher[tauchat.ChatEvent], func() []tauchat.ChatEvent) {
	t.Helper()
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	client := bus.Client("test-protocol")
	pub := eventbus.Publish[tauchat.ChatEvent](client)
	sub := eventbus.Subscribe[tauchat.ChatEvent](client)
	t.Cleanup(sub.Close)

	// Wait for the subscription to register before publishing.
	time.Sleep(10 * time.Millisecond)

	stop := make(chan struct{})
	collected := make(chan []tauchat.ChatEvent, 1)
	go func() {
		var events []tauchat.ChatEvent
		for {
			select {
			case ev := <-sub.Events():
				events = append(events, ev)
			case <-stop:
				collected <- events
				return
			}
		}
	}()

	return pub, func() []tauchat.ChatEvent {
		// readChildResult has returned by the time this is called, so
		// nothing more will be published; give the collector a brief moment
		// to drain anything already in flight.
		time.Sleep(20 * time.Millisecond)
		close(stop)
		return <-collected
	}
}

// TestReadChildResult_AuthorityFilteringOnPublish pins that an agent.event
// whose instance field does not match the pipe binding is dropped (counted
// as a violation) and never re-published, while a matching event is
// forwarded as a ChildAgentMessageEvent.
func TestReadChildResult_AuthorityFilteringOnPublish(t *testing.T) {
	pub, stopAndCollect := collectChildEvents(t)

	msgs := []wireMsg{
		childEvent("evil#2"), // authority violation: not published
		childEvent(protocolTestInstance),
		childResult("completed", "done"),
	}
	_, res, _, err := readChildResult(context.Background(), feedChild(t, msgs), protocolTestInstance, "call-1", "parent-session", pub)
	require.NoError(t, err)
	require.Equal(t, "completed", res.Status)

	var forwarded []tauchat.ChildAgentMessageEvent
	for _, ev := range stopAndCollect() {
		if msg, ok := ev.(tauchat.ChildAgentMessageEvent); ok {
			forwarded = append(forwarded, msg)
		}
	}
	require.Len(t, forwarded, 1, "only the instance-matching event may be forwarded")
	require.Equal(t, protocolTestInstance, forwarded[0].InstanceID)
	require.Equal(t, "call-1", forwarded[0].CallID)
	_, ok := forwarded[0].Event.(tauchat.ChatResponseStartedEvent)
	require.True(t, ok, "expected decoded ChatResponseStartedEvent, got %T", forwarded[0].Event)
}
