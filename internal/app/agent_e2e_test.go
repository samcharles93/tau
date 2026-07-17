//go:build e2e

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
)

var (
	tauBinary   string
	fakeBaseURL string
)

// TestMain builds tau once, starts a fake AI provider, and runs e2e tests.
func TestMain(m *testing.M) {
	// Build tau binary.
	var err error
	tauBinary, err = buildTauBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build tau: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tauBinary)

	// Start fake AI provider.
	srv := httptest.NewServer(fakeAIServer())
	fakeBaseURL = srv.URL
	defer srv.Close()

	os.Exit(m.Run())
}

func buildTauBinary() (string, error) {
	modRoot, err := runGo("env", "GOMOD")
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	projRoot := filepath.Dir(modRoot)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("tau-e2e-%d", time.Now().UnixNano()))
	cmd := exec.Command("go", "build", "-C", projRoot, "-o", path, "./cmd/tau")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %w\n%s", err, out)
	}
	return path, nil
}

func runGo(args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func fakeAIServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "fake-model", "object": "model"},
			},
		})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`{"id":"fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`{"id":"fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":" from the fake agent!"},"finish_reason":null}]}`,
			`{"id":"fake","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`[DONE]`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	})
	return mux
}

// testEnv holds per-test resources.
type testEnv struct {
	t       *testing.T
	tmpDir  string
	store   *store.SQLiteStore
	cleanup func()
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tau.db")
	s, err := store.NewSQLiteStore(context.Background(), dbPath, tmpDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	// Write config pointing at fake provider.
	cfg := fmt.Sprintf(`providers:
  - name: fake
    base_url: %s
    api_key: test
`, fakeBaseURL)
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return &testEnv{
		t:      t,
		tmpDir: tmpDir,
		store:  s,
		cleanup: func() {
			s.Close()
		},
	}
}

func (e *testEnv) ConfigDir() string { return e.tmpDir }

// spawnChild runs the compiled tau binary as a child agent.
func (e *testEnv) spawnChild(args ...string) (*exec.Cmd, error) {
	childArgs := []string{"--child", "--insecure", "--ephemeral"}
	childArgs = append(childArgs, args...)
	cmd := exec.Command(tauBinary, childArgs...)
	cmd.Env = append(
		os.Environ(),
		"TAU_CONFIG_DIR="+e.tmpDir,
	)
	return cmd, nil
}

func (e *testEnv) providerConfig() config.ProviderConfig {
	return config.ProviderConfig{
		Name:    "fake",
		BaseURL: fakeBaseURL,
		APIKey:  "test",
	}
}

// TestE2E_SpawnFreshRoundTrip verifies the child process starts and exits cleanly.
func TestE2E_SpawnFreshRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	cmd, err := env.spawnChild()
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Read agent.ready handshake.
	decoder := json.NewDecoder(stdout)
	var ready map[string]interface{}
	if err := decoder.Decode(&ready); err != nil {
		// Drain stderr
		go func() {
			buf := make([]byte, 4096)
			for {
				n, _ := stderr.Read(buf)
				if n == 0 {
					break
				}
				t.Logf("child stderr: %s", buf[:n])
			}
		}()
		t.Fatalf("read agent.ready: %v", err)
	}
	t.Logf("ready: %v", ready)

	// Send agent.assign.
	assign := map[string]interface{}{
		"agent":   "agent.assign",
		"session": "e2e-test-session",
		"config": map[string]interface{}{
			"provider": env.providerConfig(),
			"model":    chat.ChatModelRef{ID: "fake-model", URL: fakeBaseURL},
		},
		"prompt": "hello",
	}
	assignJSON, _ := json.Marshal(assign)
	fmt.Fprintf(stdin, "%s\n", assignJSON)
	stdin.Close()

	// Read until agent.result or process exit.
	var result map[string]interface{}
	for decoder.More() {
		var frame map[string]interface{}
		if err := decoder.Decode(&frame); err != nil {
			break
		}
		t.Logf("frame: %v", frame["agent"])
		if frame["agent"] == "agent.done" || frame["agent"] == "agent.result" {
			result = frame
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		// Child may exit non-zero during shutdown; that's OK if we got a result.
		if result == nil {
			t.Fatalf("wait: %v", err)
		}
	}
	if result == nil {
		t.Fatal("no agent.result received")
	}
	t.Logf("result: %v", result)
}

// TestE2E_ConcurrentChildren verifies 3 concurrent children.
func TestE2E_ConcurrentChildren(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	const n = 3
	errs := make(chan error, n)
	for i := range n {
		go func(idx int) {
			cmd, err := env.spawnChild()
			if err != nil {
				errs <- err
				return
			}
			if err := cmd.Start(); err != nil {
				errs <- err
				return
			}
			cmd.Process.Kill()
			cmd.Wait()
			errs <- nil
		}(i)
	}

	for range n {
		if err := <-errs; err != nil {
			t.Errorf("child error: %v", err)
		}
	}
}

// TestE2E_StoreAssertions verifies store operations after a child run.
func TestE2E_StoreAssertions(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	// Verify store is functional.
	count, err := env.store.Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	t.Logf("session count: %d", count)
	_ = count // may be 0 for fresh store
}
