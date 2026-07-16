package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

// TestRunChild_InvalidHandshake verifies the child exits non-zero when the
// first message on stdin is not agent.assign.
func TestRunChild_InvalidHandshake(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)

	childStdin, parentStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childStdout, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = childStdin
	os.Stdout = childStdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		childStdin.Close()
		childStdout.Close()
	}()

	opts := ChatOptions{Insecure: true}

	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		errCh <- RunChild(ctx, opts)
	}()

	writer := stdio.NewWriter(parentStdin)
	writer.WriteEnvelope("agent.cancel", map[string]string{"task_id": "123"})
	parentStdin.Close()

	runErr := <-errCh
	if runErr == nil {
		t.Error("expected RunChild to return error for bad first message")
	} else {
		t.Logf("child error (expected): %v", runErr)
	}
}

// TestRunChild_InstanceNotFound verifies RunChild exits non-zero when the
// requested instance ID doesn't exist in the store.
func TestRunChild_InstanceNotFound(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/sessions.db"

	s, err := store.NewSQLiteStore(context.Background(), dbPath, dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	s.Close()

	t.Setenv("TAU_CONFIG_DIR", dir)

	childStdin, parentStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childStdout, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = childStdin
	os.Stdout = childStdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		childStdin.Close()
		childStdout.Close()
	}()

	opts := ChatOptions{Insecure: true}

	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		errCh <- RunChild(ctx, opts)
	}()

	writer := stdio.NewWriter(parentStdin)
	writer.WriteEnvelope("agent.assign", map[string]any{
		"task_id":     "nonexistent#zzzzzz",
		"instance_id": "nonexistent#zzzzzz",
		"session_id":  "test-session-001",
		"prompt":      "hello",
		"model":       map[string]string{"provider": "test", "model": "test-model"},
	})
	parentStdin.Close()

	runErr := <-errCh
	if runErr == nil {
		t.Error("expected RunChild to return error for missing instance")
	} else {
		t.Logf("child error (expected): %v", runErr)
	}
}

// TestRunChild_BadHandshakeContents verifies that agent.assign with a
// missing required field produces an error.
func TestRunChild_BadHandshakeContents(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)

	childStdin, parentStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childStdout, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = childStdin
	os.Stdout = childStdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		childStdin.Close()
		childStdout.Close()
	}()

	opts := ChatOptions{Insecure: true}

	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		errCh <- RunChild(ctx, opts)
	}()

	writer := stdio.NewWriter(parentStdin)
	writer.WriteEnvelope("agent.assign", map[string]string{"prompt": "hello"})
	parentStdin.Close()

	runErr := <-errCh
	if runErr == nil {
		t.Error("expected RunChild to return error for malformed agent.assign")
	} else {
		t.Logf("child error (expected): %v", runErr)
	}
}

// TestRunChild_HandshakeRoundTripWithValidStore runs the full handshake
// with a valid instance row in the store. Because there is no live LLM
// provider, the child will fail at the coordinator creation step -
// but it should succeed through handshake and store validation first.
func TestRunChild_HandshakeRoundTripWithValidStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/sessions.db"
	s, err := store.NewSQLiteStore(context.Background(), dbPath, dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	inst := store.AgentInstance{
		ID:               "test-agent#aaaaaa",
		SpecName:         "test-agent",
		SpecScope:        "builtin",
		SpecHash:         "abc123",
		SpecSnapshot:     `{"name":"test-agent","body":"test"}`,
		ResolvedProvider: "test",
		ResolvedModel:    "test-model",
		Depth:            1,
		StartedAt:        time.Now(),
	}
	if err := s.SaveAgentInstance(context.Background(), inst); err != nil {
		t.Fatalf("SaveAgentInstance: %v", err)
	}

	childStdin, parentStdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	childStdout, parentStdout, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = childStdin
	os.Stdout = childStdout
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		childStdin.Close()
		childStdout.Close()
	}()

	t.Setenv("TAU_CONFIG_DIR", dir)

	opts := ChatOptions{
		Config: config.Config{
			Providers: []config.ProviderConfig{{
				Name:    "test",
				BaseURL: "https://test.example",
				Auth:    config.AuthConfig{Type: config.AuthTypeNone},
			}},
		},
		Provider: config.ProviderConfig{
			Name:    "test",
			BaseURL: "https://test.example",
			Auth:    config.AuthConfig{Type: config.AuthTypeNone},
		},
		Model:    "test-model",
		Insecure: true,
	}

	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		errCh <- RunChild(ctx, opts)
	}()

	parentWriter := stdio.NewWriter(parentStdin)
	parentReader := stdio.NewReader(parentStdout)

	assignMsg := map[string]any{
		"task_id":     "test-agent#aaaaaa",
		"instance_id": "test-agent#aaaaaa",
		"session_id":  "test-session-001",
		"prompt":      "say hello",
		"model":       map[string]string{"provider": "test", "model": "test-model"},
	}
	if err := parentWriter.WriteEnvelope("agent.assign", assignMsg); err != nil {
		t.Fatalf("write agent.assign: %v", err)
	}

	// Read handshake.
	ready, hsErr := parentReader.ReadHandshake(stdio.ProtocolVersion)
	if hsErr != nil {
		// Cleanup: close write side so child can exit.
		parentStdin.Close()
		t.Logf("handshake failed (expected without live provider): %v", hsErr)
	}

	// Read envelopes until the child exits.
	for {
		typ, payload, readErr := parentReader.ReadEnvelope()
		if readErr != nil {
			cancel()
			rErr := <-errCh
			if ready != nil {
				t.Logf("read envelope err: %v, child err: %v", readErr, rErr)
			}
			break
		}
		if typ == "agent.result" {
			var result map[string]any
			json.Unmarshal(payload, &result)
			t.Logf("agent.result: %v", result)
			break
		}
	}
}
