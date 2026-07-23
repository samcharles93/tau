package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/updater"
)

// fakeUpdateServer returns a server that responds immediately.
func fakeUpdateServer(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	return fakeUpdateServerWithDelay(t, tagName, 0)
}

// fakeUpdateServerWithDelay returns a server whose /releases/latest handler
// sleeps for the given duration before responding.
func fakeUpdateServerWithDelay(t *testing.T, tagName string, delay time.Duration) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/samcharles93/tau/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		fmt.Fprintf(w, `{"tag_name": %q, "html_url": %q}`, tagName, srv.URL+"/releases/tag/"+tagName)
	})

	return srv
}

// setTestConfigDir points tauconfig.Dir() at a temporary directory for the
// duration of the test.
func setTestConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	return dir
}

// writeUpdateCacheFile writes a cache file with the given last-check time.
func writeUpdateCacheFile(t *testing.T, dir string, lastCheck time.Time) {
	t.Helper()
	c := updateCache{LastCheck: lastCheck.UTC().Format(time.RFC3339)}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "update-cache.json"), data, 0o644))
}

// collectNotifications subscribes to ChatEvent on the bus and collects
// ChatNotificationEvent values until the returned channel is closed.
func collectNotifications(t *testing.T, bus *eventbus.Bus) <-chan tauchat.ChatNotificationEvent {
	t.Helper()

	subClient := bus.Client("update-check-test")
	sub := eventbus.Subscribe[tauchat.ChatEvent](subClient)
	ch := make(chan tauchat.ChatNotificationEvent, 1)

	go func() {
		defer close(ch)
		defer sub.Close()
		defer subClient.Close()
		for ev := range sub.Events() {
			if notif, ok := ev.(tauchat.ChatNotificationEvent); ok {
				ch <- notif
			}
		}
	}()

	return ch
}

// drainNotifications drains and returns any notifications from ch with a
// short timeout.  Returns nil if none arrive.
func drainNotifications(t *testing.T, ch <-chan tauchat.ChatNotificationEvent) *tauchat.ChatNotificationEvent {
	t.Helper()
	select {
	case n := <-ch:
		return &n
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func TestBackgroundUpdateCheck_Disabled(t *testing.T) {
	dir := setTestConfigDir(t)
	server := fakeUpdateServer(t, "v2.0.0")
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	// Start the check in a goroutine; it should return immediately because
	// updates.mode is "disabled".
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{Mode: "disabled"}, bus)
	}()

	select {
	case <-done:
		// ok — returned immediately
	case <-time.After(time.Second):
		t.Fatal("StartBackgroundUpdateCheck did not return promptly when disabled")
	}

	// No notification should be published.
	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected when updates are disabled")

	// No cache file should be written.
	_, err := os.Stat(filepath.Join(dir, "update-cache.json"))
	assert.True(t, os.IsNotExist(err), "cache file should not exist when disabled")
}

func TestBackgroundUpdateCheck_DevBuild(t *testing.T) {
	dir := setTestConfigDir(t)
	server := fakeUpdateServer(t, "v2.0.0")
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "dev", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartBackgroundUpdateCheck did not return promptly for dev build")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif)

	// No cache file should be written for dev builds.
	_, err := os.Stat(filepath.Join(dir, "update-cache.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestBackgroundUpdateCheck_CacheFresh(t *testing.T) {
	dir := setTestConfigDir(t)
	// Write a cache file from 1 hour ago.
	writeUpdateCacheFile(t, dir, time.Now().Add(-time.Hour))

	server := fakeUpdateServer(t, "v2.0.0")
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartBackgroundUpdateCheck did not return promptly when cache is fresh")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected with fresh cache")
}

func TestBackgroundUpdateCheck_NoUpdate(t *testing.T) {
	dir := setTestConfigDir(t)
	server := fakeUpdateServer(t, "v1.0.0") // same version
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartBackgroundUpdateCheck timed out")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected when already up to date")

	// Cache should be updated even when no update is available.
	_, err := os.Stat(filepath.Join(dir, "update-cache.json"))
	assert.NoError(t, err, "cache file should exist after check")
}

func TestBackgroundUpdateCheck_NewerVersion(t *testing.T) {
	dir := setTestConfigDir(t)
	server := fakeUpdateServer(t, "v2.0.0")
	// Override the API base URL so updater.Run talks to our fake server.
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartBackgroundUpdateCheck timed out")
	}

	// A notification should be published.
	notif := drainNotifications(t, notifs)
	require.NotNil(t, notif, "expected a notification when a newer version exists")
	assert.Equal(t, tauchat.ChatNotificationInfo, notif.Level)
	assert.Contains(t, notif.Message, "v2.0.0")
	assert.Contains(t, notif.Message, "run tau update")

	// Cache should be updated.
	_, err := os.Stat(filepath.Join(dir, "update-cache.json"))
	assert.NoError(t, err)
}

func TestBackgroundUpdateCheck_Timeout(t *testing.T) {
	setTestConfigDir(t)
	// Server that hangs longer than the 3 s timeout.
	server := fakeUpdateServerWithDelay(t, "v2.0.0", 10*time.Second)
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
		// ok — returned after HTTP timeout
	case <-time.After(4 * time.Second):
		t.Fatal("StartBackgroundUpdateCheck did not return within expected timeout window")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected when HTTP times out")
}

func TestBackgroundUpdateCheck_ContextCancelled(t *testing.T) {
	setTestConfigDir(t)
	// Server that hangs — we'll cancel the context before the HTTP call
	// completes.
	server := fakeUpdateServerWithDelay(t, "v2.0.0", 10*time.Second)
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	// Give the goroutine time to start the HTTP call, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok — returned cleanly after cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("StartBackgroundUpdateCheck did not return after context cancellation")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected when context is cancelled")
}

func TestBackgroundUpdateCheck_ServerError(t *testing.T) {
	setTestConfigDir(t)
	// Server returns 500.
	server := fakeUpdateServer(t, "") // empty tag → 500
	t.Setenv("TAU_UPDATE_API_URL", server.URL)

	bus := eventbus.New()
	defer bus.Close()

	notifs := collectNotifications(t, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		StartBackgroundUpdateCheck(ctx, "v1.0.0", tauconfig.UpdatesConfig{}, bus)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StartBackgroundUpdateCheck timed out on server error")
	}

	notif := drainNotifications(t, notifs)
	assert.Nil(t, notif, "no notification expected on server error")
}

func TestCurrentTagFromVersion(t *testing.T) {
	assert.Equal(t, "v1.2.3", currentTagFromVersion("v1.2.3 (abc, 2026-07-10)"))
	assert.Equal(t, "v1.0.0", currentTagFromVersion("v1.0.0"))
	assert.Equal(t, "", currentTagFromVersion(""))
	assert.Equal(t, "", currentTagFromVersion(strings.TrimSpace(" ")))
	assert.Equal(t, "dev", currentTagFromVersion("dev"))
}

func TestIsDevelBuild(t *testing.T) {
	assert.True(t, isDevelBuild("dev"))
	assert.True(t, isDevelBuild("v0.30.0-dev"))
	assert.True(t, isDevelBuild("none"))
	assert.False(t, isDevelBuild("v1.2.3"))
	assert.False(t, isDevelBuild("v1.2.3 (built 2026-07-10T10:00:00Z)"))
}

func TestCacheFresh(t *testing.T) {
	dir := setTestConfigDir(t)
	cachePath := filepath.Join(dir, "update-cache.json")

	// No file — not fresh.
	assert.False(t, cacheFresh(cachePath))

	// File written 1 hour ago — fresh.
	writeUpdateCacheFile(t, dir, time.Now().Add(-time.Hour))
	assert.True(t, cacheFresh(cachePath))

	// File written 25 hours ago — stale.
	writeUpdateCacheFile(t, dir, time.Now().Add(-25*time.Hour))
	assert.False(t, cacheFresh(cachePath))

	// Corrupt JSON — not fresh.
	require.NoError(t, os.WriteFile(cachePath, []byte("{bad"), 0o644))
	assert.False(t, cacheFresh(cachePath))

	// Invalid timestamp — not fresh.
	require.NoError(t, os.WriteFile(cachePath, []byte(`{"last_check":"not-a-time"}`), 0o644))
	assert.False(t, cacheFresh(cachePath))
}

func TestWriteAndReadUpdateCache(t *testing.T) {
	dir := setTestConfigDir(t)
	cachePath := filepath.Join(dir, "update-cache.json")

	// Write cache.
	writeUpdateCache(cachePath)
	assert.True(t, cacheFresh(cachePath))

	// Read back.
	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	var c updateCache
	require.NoError(t, json.Unmarshal(data, &c))

	_, err = time.Parse(time.RFC3339, c.LastCheck)
	assert.NoError(t, err, "last_check must be a valid RFC 3339 timestamp")
}

// Verify the updater package exports referenced in update_check.go compile.
func TestUpdaterExports(t *testing.T) {
	// Compile-time check: these symbols must exist.
	_ = updater.ErrNoUpdate
	_ = updater.ErrDevBuild
	_ = updater.Options{CheckOnly: true}
}
