package metrics_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

func TestFileSubscriber_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)
	defer fs.Close()

	pub := eventbus.Publish[chat.MetricEvent](bus.Client("pub"))
	pub.Publish(chat.MetricEvent{
		Category:  chat.MetricCategoryLLM,
		Name:      "llm.response",
		Value:     42,
		Unit:      "tokens",
		Labels:    map[string]string{"provider": "openai", "model": "gpt-x"},
		SessionID: "s1",
	})
	pub.Publish(chat.MetricEvent{
		Category:  chat.MetricCategoryTool,
		Name:      "tool.read.duration",
		Value:     120,
		Unit:      "ms",
		Labels:    map[string]string{"tool": "read", "status": "success"},
		SessionID: "s1",
	})

	// Wait for the bus pump to deliver events to the subscriber.
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
		if err != nil {
			return false
		}
		return len(strings.Split(strings.TrimSpace(string(data)), "\n")) == 2
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, fs.Close())

	// Verify the events after close.
	data, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 2, "expected 2 JSONL lines")

	var e1, e2 chat.MetricEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &e1))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &e2))

	require.Equal(t, chat.MetricCategoryLLM, e1.Category)
	require.Equal(t, "llm.response", e1.Name)
	require.Equal(t, float64(42), e1.Value)
	require.Equal(t, "s1", e1.SessionID)

	require.Equal(t, chat.MetricCategoryTool, e2.Category)
	require.Equal(t, "tool.read.duration", e2.Name)
}

func TestFileSubscriberCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "nested", "metrics")
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)
	defer fs.Close()

	require.DirExists(t, dir)
	require.FileExists(t, filepath.Join(dir, "metrics.jsonl"))
}

func TestFileSubscriberCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)

	// First close should succeed.
	require.NoError(t, fs.Close())
	// Second close is safe (guarded by closed flag).
	require.NoError(t, fs.Close())
}

func TestFileSubscriberHandlesEmptyEventGracefully(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)
	defer fs.Close()

	// Publish an event with all zero values (valid, just boring).
	pub := eventbus.Publish[chat.MetricEvent](bus.Client("pub"))
	pub.Publish(chat.MetricEvent{})

	// Give the bus time to deliver.
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, fs.Close())

	data, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1, "expected exactly 1 JSONL line for the empty event")

	var e chat.MetricEvent
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &e))
	require.Empty(t, e.Category)
	require.Empty(t, e.Name)
}

func TestFileSubscriberConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.New()
	t.Cleanup(bus.Close)

	fs, err := metrics.NewFileSubscriber(bus.Client("file"), dir)
	require.NoError(t, err)
	defer fs.Close()

	pub := eventbus.Publish[chat.MetricEvent](bus.Client("pub"))

	// Publish 50 events - the bus serialises delivery so no data races
	// on the file write, but this validates the mutex path.
	for i := range 50 {
		pub.Publish(chat.MetricEvent{
			Category:  chat.MetricCategoryTool,
			Name:      "tool.read.duration",
			Value:     float64(i),
			Unit:      "ms",
			SessionID: "s1",
		})
	}

	// Wait for the bus to deliver all events.
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
		if err != nil {
			return false
		}
		return len(strings.Split(strings.TrimSpace(string(data)), "\n")) == 50
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, fs.Close())

	data, err := os.ReadFile(filepath.Join(dir, "metrics.jsonl"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 50)

	// Verify each line is valid JSON and values are sequential.
	for i, line := range lines {
		var e chat.MetricEvent
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		require.Equal(t, float64(i), e.Value)
	}
}
