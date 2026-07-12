package stdio

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	msg := map[string]any{"hello": "world", "num": 42}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	data, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["hello"] != "world" || got["num"].(float64) != 42 {
		t.Fatalf("unexpected content: %v", got)
	}
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	for i := range 5 {
		if err := w.WriteMessage(map[string]any{"seq": i}); err != nil {
			t.Fatalf("WriteMessage %d: %v", i, err)
		}
	}

	for i := range 5 {
		data, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage %d: %v", i, err)
		}
		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %d: %v", i, err)
		}
		if got["seq"].(float64) != float64(i) {
			t.Fatalf("expected seq %d, got %v", i, got["seq"])
		}
	}
}

func TestReadEnvelope(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	env := rawEnvelope{
		Type:    "agent.ready",
		Payload: json.RawMessage(`{"instance":"test#abc123","pid":42,"protocol":1}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	// Write using raw bytes to avoid double-enveloping
	w.bw.Write(data)
	w.bw.Write([]byte{'\n'})
	w.bw.Flush()

	r := NewReader(&buf)
	typ, payload, err := r.ReadEnvelope()
	if err != nil {
		t.Fatalf("ReadEnvelope: %v", err)
	}
	if typ != "agent.ready" {
		t.Fatalf("expected type agent.ready, got %q", typ)
	}
	var ready AgentReady
	if err := json.Unmarshal(payload, &ready); err != nil {
		t.Fatalf("unmarshal agent.ready: %v", err)
	}
	if ready.Instance != "test#abc123" {
		t.Fatalf("expected instance test#abc123, got %q", ready.Instance)
	}
}

func TestWriteRaw(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	data := json.RawMessage(`{"key":"value"}`)
	if err := w.WriteRaw(data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	got, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %s, got %s", data, got)
	}
}

func TestReadEOF(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.WriteMessage(map[string]any{"last": true})

	r := NewReader(&buf)
	data, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got map[string]any
	json.Unmarshal(data, &got)
	if got["last"] != true {
		t.Fatal("expected last message")
	}

	_, err = r.ReadMessage()
	if err != io.EOF {
		t.Fatalf("expected io.EOF after last message, got %v", err)
	}
}

func TestReadOverSizeLine(t *testing.T) {
	// Create a line larger than MaxLineSize
	big := strings.Repeat("x", MaxLineSize+1)
	var buf bytes.Buffer
	buf.WriteString(big)
	buf.WriteByte('\n')

	r := NewReader(&buf)
	_, err := r.ReadMessage()
	if err == nil {
		t.Fatal("expected error for oversize line")
	}
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
}

func TestWriteOverSizeMessage(t *testing.T) {
	big := strings.Repeat("x", MaxLineSize+1)
	msg := map[string]any{"big": big}
	var buf bytes.Buffer
	w := NewWriter(&buf)
	err := w.WriteMessage(msg)
	if err == nil {
		t.Fatal("expected error for oversize message")
	}
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}
}

func TestHandshakeSuccess(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	if err := w.WriteHandshake("test#abc123", 42); err != nil {
		t.Fatalf("WriteHandshake: %v", err)
	}

	ready, err := r.ReadHandshake(ProtocolVersion)
	if err != nil {
		t.Fatalf("ReadHandshake: %v", err)
	}
	if ready.Instance != "test#abc123" {
		t.Fatalf("expected instance test#abc123, got %q", ready.Instance)
	}
	if ready.PID != 42 {
		t.Fatalf("expected pid 42, got %d", ready.PID)
	}
}

func TestHandshakeBadFirstMessage(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Write an enveloped message with a non-ready type
	w.WriteEnvelope("not.ready", map[string]any{"key": "value"})

	r := NewReader(&buf)
	_, err := r.ReadHandshake(ProtocolVersion)
	if err == nil {
		t.Fatal("expected error for bad first message")
	}
	if !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected ErrHandshakeFailed, got %v", err)
	}
}

func TestHandshakeVersionMismatch(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	// Write a ready with wrong version
	if err := w.WriteHandshake("test#abc123", 42); err != nil {
		t.Fatalf("WriteHandshake: %v", err)
	}

	_, err := r.ReadHandshake(ProtocolVersion + 1)
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
	if !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("expected ErrProtocolVersion, got %v", err)
	}
}

func TestIsHandshakeError(t *testing.T) {
	if !IsHandshakeError(ErrHandshakeFailed) {
		t.Error("IsHandshakeError(ErrHandshakeFailed) should be true")
	}
	if !IsHandshakeError(ErrProtocolVersion) {
		t.Error("IsHandshakeError(ErrProtocolVersion) should be true")
	}
	if IsHandshakeError(io.EOF) {
		t.Error("IsHandshakeError(io.EOF) should be false")
	}
}

func TestIsEOFOrCancel(t *testing.T) {
	if !IsEOFOrCancel(io.EOF) {
		t.Error("IsEOFOrCancel(io.EOF) should be true")
	}
	if IsEOFOrCancel(errors.New("other")) {
		t.Error("IsEOFOrCancel(other) should be false")
	}
}
