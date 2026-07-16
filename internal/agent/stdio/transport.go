// Package stdio implements the agent-wire stdio JSONL transport defined in
// docs/specs/agents/03-wire-protocol.md. It provides line-framed
// reader/writer pairs with a 8 MiB line cap, handshake enforcement
// (first message must be agent.ready with a matching protocol version),
// and EOF semantics for both parent and child sides.
package stdio

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the current agent-wire protocol version. The parent
// checks this against the child's agent.ready at handshake time. Additive
// payload fields do not bump it; structural handshake changes do.
const ProtocolVersion = 1

// MaxLineSize is the maximum encoded envelope size (8 MiB). Anything larger
// belongs in the store, not on the wire (data-plane rule, see 04-storage).
const MaxLineSize = 8 * 1024 * 1024

// ErrLineTooLong is returned when a line exceeds MaxLineSize.
var ErrLineTooLong = errors.New("line exceeds maximum size (8 MiB)")

// ErrHandshakeFailed is returned when the first message from the child is
// not a valid agent.ready or has an unsupported protocol version.
var ErrHandshakeFailed = errors.New("handshake failed: first message must be agent.ready")

// ErrProtocolVersion is returned when the child's protocol version does not
// match what the parent expects.
var ErrProtocolVersion = errors.New("unsupported agent protocol version")

// rawEnvelope is a lightweight envelope used for the handshake check only.
// The full envelope with From/To lives in internal/bridge; this package
// avoids importing bridge to stay a leaf dependency.
type rawEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Writer wraps an io.Writer and writes one JSON-encoded message per line,
// flushing after each write when the underlying writer supports flushing.
// Safe for concurrent use: callers on both sides of the wire write from more
// than one goroutine (e.g. the parent's normal envelope stream and its
// cancellation supervisor both write to the same child's stdin), and
// bufio.Writer itself has no internal locking.
type Writer struct {
	w  io.Writer
	f  flusher
	bw *bufio.Writer
	mu sync.Mutex
}

type flusher interface {
	Flush() error
}

// NewWriter creates a Writer that writes LF-terminated JSON lines to w.
func NewWriter(w io.Writer) *Writer {
	bw := bufio.NewWriter(w)
	var f flusher
	if ff, ok := w.(flusher); ok {
		f = ff
	}
	return &Writer{w: w, f: f, bw: bw}
}

// WriteMessage encodes msg as JSON and writes it as a single line, flushing
// afterwards so the receiver sees the complete message immediately.
func (w *Writer) WriteMessage(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if len(data) > MaxLineSize {
		return fmt.Errorf("%w (%d bytes)", ErrLineTooLong, len(data))
	}
	return w.writeLine(data)
}

// WriteRaw writes a pre-serialised JSON byte slice as a line. The caller
// is responsible for ensuring the data fits within MaxLineSize.
func (w *Writer) WriteRaw(data []byte) error {
	if len(data) > MaxLineSize {
		return fmt.Errorf("%w (%d bytes)", ErrLineTooLong, len(data))
	}
	return w.writeLine(data)
}

// writeLine appends a newline and flushes, holding mu for the duration so
// concurrent writers never interleave partial lines on the wire.
func (w *Writer) writeLine(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.bw.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if _, err := w.bw.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	if err := w.bw.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}

// Reader wraps an io.Reader and reads one JSON-encoded message per line,
// enforcing the MaxLineSize cap.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader creates a Reader that reads LF-terminated JSON lines from r.
// The first message must be agent.ready; the caller should call
// ReadHandshake to enforce this.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), MaxLineSize)
	sc.Split(bufio.ScanLines)
	return &Reader{scanner: sc}
}

// ReadMessage reads the next line and returns the raw JSON bytes. It returns
// io.EOF when the stream is exhausted. ErrLineTooLong is returned when a
// line exceeds MaxLineSize.
func (r *Reader) ReadMessage() ([]byte, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return nil, fmt.Errorf("%w (%d bytes)", ErrLineTooLong, MaxLineSize)
			}
			return nil, fmt.Errorf("read: %w", err)
		}
		return nil, io.EOF
	}
	// Copy the bytes - the scanner reuses its buffer.
	data := r.scanner.Bytes()
	line := make([]byte, len(data))
	copy(line, data)
	return line, nil
}

// ReadEnvelope reads the next line and returns the type discriminator and
// raw payload. This is a convenience for callers that need to route on
// the type field.
func (r *Reader) ReadEnvelope() (typ string, payload json.RawMessage, err error) {
	data, err := r.ReadMessage()
	if err != nil {
		return "", nil, err
	}
	var env rawEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env.Type, env.Payload, nil
}

// Handshake helpers

// ReadHandshake reads the first message from the child. It must be an
// agent.ready with a protocol version matching expectVersion. Returns the
// parsed AgentReady on success.
func (r *Reader) ReadHandshake(expectVersion int) (*AgentReady, error) {
	typ, payload, err := r.ReadEnvelope()
	if err != nil {
		return nil, fmt.Errorf("%w: read: %w", ErrHandshakeFailed, err)
	}
	if typ != "agent.ready" {
		return nil, fmt.Errorf("%w: got %q", ErrHandshakeFailed, typ)
	}
	var ready AgentReady
	if err := json.Unmarshal(payload, &ready); err != nil {
		return nil, fmt.Errorf("%w: unmarshal agent.ready: %w", ErrHandshakeFailed, err)
	}
	if ready.Protocol != expectVersion {
		return nil, fmt.Errorf("%w: child protocol %d, parent expects %d", ErrProtocolVersion, ready.Protocol, expectVersion)
	}
	return &ready, nil
}

// WriteHandshake writes an agent.ready envelope as the first line on the
// child's stdout.
func (w *Writer) WriteHandshake(instance string, pid int) error {
	return w.WriteEnvelope("agent.ready", &AgentReady{
		Instance: instance,
		PID:      pid,
		Protocol: ProtocolVersion,
	})
}

// WriteEnvelope writes msg wrapped in a {type, payload} envelope.
func (w *Writer) WriteEnvelope(typ string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return w.WriteMessage(rawEnvelope{
		Type:    typ,
		Payload: body,
	})
}

// AgentReady is the first message a child writes on stdout.
type AgentReady struct {
	Instance string `json:"instance"`
	PID      int    `json:"pid"`
	Protocol int    `json:"protocol"`
}

// IsHandshakeError reports whether err is a handshake-related error
// (ErrHandshakeFailed or ErrProtocolVersion).
func IsHandshakeError(err error) bool {
	return errors.Is(err, ErrHandshakeFailed) || errors.Is(err, ErrProtocolVersion)
}

// IsEOFOrCancel reports whether err represents a clean end-of-stream or
// parent-initiated cancellation (stdin EOF outside the cancel flow).
func IsEOFOrCancel(err error) bool {
	return errors.Is(err, io.EOF)
}
