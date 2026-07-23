package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeReleaseTag(t *testing.T) {
	cases := map[string]string{
		"1.2.0":  "v1.2.0",
		"v1.2.0": "v1.2.0",
		"V1.2.0": "v1.2.0",
		"v0.0.1": "v0.0.1",
	}
	for in, want := range cases {
		if got := normalizeReleaseTag(in); got != want {
			t.Errorf("normalizeReleaseTag(%q) = %q, want %q", in, got, want)
		}
	}
}

// requestPathRecordingServer returns an httptest.Server that records every
// requested path and serves an empty (but valid) release JSON body for it.
func requestPathRecordingServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.2.0"})
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

// TestFetchGitHubRelease_VersionRequestPaths is the regression test for the
// tau-23d bug: an explicit "@v1.2.0" version used to be formatted into the
// GitHub tag URL as "vv1.2.0" (the code always prepended "v" without
// checking whether the caller already supplied one), while the bare
// "@1.2.0" form worked. Both documented forms must now resolve to the same
// tag exactly once.
func TestFetchGitHubRelease_VersionRequestPaths(t *testing.T) {
	srv, paths := requestPathRecordingServer(t)
	origBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = origBase })

	cases := []struct {
		name       string
		version    string
		wantSuffix string
	}{
		{"bare version", "1.2.0", "/repos/acme/widgets/releases/tags/v1.2.0"},
		{"v-prefixed version", "v1.2.0", "/repos/acme/widgets/releases/tags/v1.2.0"},
		{"empty version uses latest", "", "/repos/acme/widgets/releases/latest"},
		{"literal latest", "latest", "/repos/acme/widgets/releases/latest"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*paths = nil
			if _, err := fetchGitHubRelease(context.Background(), "acme", "widgets", tc.version); err != nil {
				t.Fatalf("fetchGitHubRelease(%q) returned error: %v", tc.version, err)
			}
			if len(*paths) != 1 || (*paths)[0] != tc.wantSuffix {
				t.Errorf("fetchGitHubRelease(%q) requested paths %v, want [%q]", tc.version, *paths, tc.wantSuffix)
			}
		})
	}
}

// TestFetchGitHubRelease_BareAndPrefixedVersionsHitSameTag proves the two
// documented input forms are not just each internally consistent but
// actually equivalent - the exact bug scenario from the issue (v1.2.0
// existed on GitHub but installing "@v1.2.0" failed with "release not
// found" because the request went to "vv1.2.0").
func TestFetchGitHubRelease_BareAndPrefixedVersionsHitSameTag(t *testing.T) {
	srv, paths := requestPathRecordingServer(t)
	origBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = origBase })

	if _, err := fetchGitHubRelease(context.Background(), "acme", "widgets", "1.2.0"); err != nil {
		t.Fatalf("bare version: unexpected error: %v", err)
	}
	bareSuffix := (*paths)[len(*paths)-1]

	if _, err := fetchGitHubRelease(context.Background(), "acme", "widgets", "v1.2.0"); err != nil {
		t.Fatalf("v-prefixed version: unexpected error: %v", err)
	}
	prefixedSuffix := (*paths)[len(*paths)-1]

	if bareSuffix != prefixedSuffix {
		t.Errorf("bare (%q) and v-prefixed (%q) versions requested different tags, want the same", bareSuffix, prefixedSuffix)
	}
}

// TestFetchGitHubRelease_NotFoundError verifies error handling is preserved
// for a genuinely missing release/tag (AC3's "assert ... error handling").
func TestFetchGitHubRelease_NotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	origBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = origBase })

	_, err := fetchGitHubRelease(context.Background(), "acme", "widgets", "v9.9.9")
	if err == nil {
		t.Fatal("expected an error for a missing release, got nil")
	}
}
