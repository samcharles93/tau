package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/updater"
)

// updateCache is the JSON schema for the update-check cache file.
type updateCache struct {
	LastCheck string `json:"last_check"`
}

// updateCachePath returns the filesystem path to the update-cache file.
func updateCachePath() string {
	return filepath.Join(tauconfig.Dir(), "update-cache.json")
}

// StartBackgroundUpdateCheck checks for a newer tau release in the
// background. It must be called in its own goroutine. It creates a
// dedicated bus client, publishes a ChatNotificationEvent if an update
// is available, and closes its client on return.
//
// The function is designed to never panic, never block startup, and
// degrade gracefully on any error (all failures are logged at debug
// level only).
func StartBackgroundUpdateCheck(ctx context.Context, version string, updatesCfg tauconfig.UpdatesConfig, bus *eventbus.Bus) {
	// 1. Eligibility: disabled mode.
	if updatesCfg.Mode == "disabled" {
		slog.Debug("update check skipped: updates disabled in config")
		return
	}

	// 2. Eligibility: dev build.
	if isDevelBuild(version) {
		slog.Debug("update check skipped: dev build")
		return
	}

	// 3. Cache: skip if checked within the last 24 hours.
	cachePath := updateCachePath()
	if cacheFresh(cachePath) {
		slog.Debug("update check skipped: cache is fresh")
		return
	}

	// 4. Fetch the latest release (CheckOnly, 3 s timeout).
	childCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Allow tests to override the API base URL via an environment
	// variable; in production this is empty and the updater default
	// (api.github.com) is used.
	apiBaseURL := os.Getenv("TAU_UPDATE_API_URL")

	httpClient := &http.Client{Timeout: 3 * time.Second}
	result, err := updater.Run(childCtx, updater.Options{
		CurrentVersion: version,
		CheckOnly:      true,
		HTTPClient:     httpClient,
		APIBaseURL:     apiBaseURL,
	})
	if err != nil {
		// No update available or dev build — update cache and return
		// without notifying.
		if err == updater.ErrNoUpdate || err == updater.ErrDevBuild {
			writeUpdateCache(cachePath)
			return
		}
		slog.Debug("update check failed", "err", err)
		return
	}

	// 5. Compare versions: defensive check that the server version
	//    actually differs from the current version.
	current := currentTagFromVersion(version)
	if result.TargetVersion == "" || result.TargetVersion == current {
		writeUpdateCache(cachePath)
		return
	}

	// 6. Publish notification through a dedicated bus client.
	client := bus.Client("update-check")
	defer client.Close()

	pub := eventbus.Publish[tauchat.ChatEvent](client)
	pub.Publish(tauchat.ChatNotificationEvent{
		Message:    "tau " + result.TargetVersion + " is available — run tau update",
		Level:      tauchat.ChatNotificationInfo,
		OccurredAt: time.Now().UTC(),
	})

	// 7. Update cache.
	writeUpdateCache(cachePath)
}

// NewUpdateChecker returns the /update handler for the TUI: an on-demand
// release check that reports its outcome as a single line. It ignores the
// 24-hour cache StartBackgroundUpdateCheck uses, because the user asked.
// Installing is still `tau update` - swapping the binary underneath a running
// session is not something a slash command should do.
func NewUpdateChecker(version string, updatesCfg tauconfig.UpdatesConfig) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if updatesCfg.Mode == "disabled" {
			return "update checks are disabled (updates.mode: disabled)", nil
		}

		childCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		result, err := updater.Run(childCtx, updater.Options{
			CurrentVersion: version,
			CheckOnly:      true,
			HTTPClient:     &http.Client{Timeout: 10 * time.Second},
			APIBaseURL:     os.Getenv("TAU_UPDATE_API_URL"),
		})
		switch {
		case errors.Is(err, updater.ErrNoUpdate):
			return "tau is already up to date (" + currentTagFromVersion(version) + ")", nil
		case errors.Is(err, updater.ErrDevBuild):
			return "dev build - update checks only apply to release builds", nil
		case err != nil:
			return "", err
		}
		if result.TargetVersion == "" || result.TargetVersion == currentTagFromVersion(version) {
			return "tau is already up to date (" + currentTagFromVersion(version) + ")", nil
		}
		return "tau " + result.TargetVersion + " is available - run `tau update` to install", nil
	}
}

// cacheFresh reports whether the cache file exists and was written less
// than 24 hours ago.  Cache read failures are debug-logged and treated
// as "not fresh" so the check proceeds.
func cacheFresh(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist or is unreadable — not fresh.
		return false
	}
	var c updateCache
	if err := json.Unmarshal(data, &c); err != nil {
		slog.Debug("update-check cache unreadable, treating as stale", "err", err)
		return false
	}
	t, err := time.Parse(time.RFC3339, c.LastCheck)
	if err != nil {
		slog.Debug("update-check cache timestamp unparseable, treating as stale", "err", err)
		return false
	}
	return time.Since(t) < 24*time.Hour
}

// writeUpdateCache writes the current UTC timestamp to the cache file.
// Failures are debug-logged and non-fatal.
func writeUpdateCache(path string) {
	c := updateCache{LastCheck: time.Now().UTC().Format(time.RFC3339)}
	data, err := json.Marshal(c)
	if err != nil {
		slog.Debug("update-check cache marshal failed", "err", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Debug("update-check cache write failed", "err", err)
	}
}

// isDevelBuild reports whether version indicates a development build
// (which should never check for updates).
func isDevelBuild(version string) bool {
	v := strings.ToLower(version)
	return strings.Contains(v, "dev") || strings.Contains(v, "none") || v == "dev"
}

// currentTagFromVersion extracts the leading version tag from a version
// string (e.g. "v1.2.3 (built 2026-07-10T...)" → "v1.2.3").
func currentTagFromVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if token, _, ok := strings.Cut(version, " "); ok {
		return token
	}
	return version
}
