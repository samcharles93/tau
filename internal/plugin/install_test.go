package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/plugin/registry"
)

func binName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// ---------- ValidatePluginName / resolvePluginBinaryPath (AC1) ----------

func TestValidatePluginName(t *testing.T) {
	valid := []string{"mcp", "tau-plugin-mcp", "a", "a.b", "a_b", "A1", "123abc"}
	for _, name := range valid {
		if err := ValidatePluginName(name); err != nil {
			t.Errorf("ValidatePluginName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"/etc/passwd",
		"a/b",
		`a\b`,
		"../../etc/passwd",
		"C:evil",
		`\\server\share`,
		".hidden",
		"-leading-dash",
		" ",
		"a b",
	}
	for _, name := range invalid {
		if err := ValidatePluginName(name); err == nil {
			t.Errorf("ValidatePluginName(%q) = nil, want error", name)
		}
	}
}

func TestResolvePluginBinaryPath_TraversalRejected(t *testing.T) {
	dir := t.TempDir()

	names := []string{"", ".", "..", "../escape", "a/../../escape", "/etc/passwd", `a\..\..\escape`}
	for _, name := range names {
		if _, err := resolvePluginBinaryPath(dir, name); err == nil {
			t.Errorf("resolvePluginBinaryPath(%q) = nil error, want rejection", name)
		}
	}
}

func TestResolvePluginBinaryPath_StaysInsidePluginsDir(t *testing.T) {
	dir := t.TempDir()
	got, err := resolvePluginBinaryPath(dir, "mcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, binName("mcp"))
	if got != want {
		t.Errorf("resolvePluginBinaryPath = %q, want %q", got, want)
	}
}

// ---------- copyLimited (AC2) ----------

func TestCopyLimited_WithinLimit(t *testing.T) {
	var buf strings.Builder
	n, err := copyLimited(&buf, strings.NewReader("hello"), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 || buf.String() != "hello" {
		t.Errorf("got n=%d buf=%q, want n=5 buf=%q", n, buf.String(), "hello")
	}
}

func TestCopyLimited_ExceedsLimit(t *testing.T) {
	var buf strings.Builder
	_, err := copyLimited(&buf, strings.NewReader("hello world"), 5)
	if err == nil {
		t.Fatal("expected error for oversized payload, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds size limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------- atomicReplace (AC3/AC4) ----------

func TestAtomicReplace_FreshInstall(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "plugin")

	tmp, err := os.CreateTemp(dir, "staged-*")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("new-binary")
	tmp.Close()

	if err := atomicReplace(tmp.Name(), dest); err != nil {
		t.Fatalf("atomicReplace failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Errorf("dest content = %q, want %q", got, "new-binary")
	}
	if _, err := os.Stat(dest + ".bak"); !os.IsNotExist(err) {
		t.Error("expected no leftover .bak file after a successful fresh install")
	}
}

func TestAtomicReplace_ReplacesExistingAndCleansBackup(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "plugin")
	if err := os.WriteFile(dest, []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	tmp, err := os.CreateTemp(dir, "staged-*")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("new-binary")
	tmp.Close()

	if err := atomicReplace(tmp.Name(), dest); err != nil {
		t.Fatalf("atomicReplace failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Errorf("dest content = %q, want %q", got, "new-binary")
	}
	if _, err := os.Stat(dest + ".bak"); !os.IsNotExist(err) {
		t.Error("expected the backup to be cleaned up after a successful replace")
	}
}

// TestAtomicReplace_RestoresPriorBinaryOnFailure verifies AC3: a failed
// replacement (here, the staged source vanishing between staging and
// activation) leaves the previously installed plugin intact rather than
// half-replaced or missing.
func TestAtomicReplace_RestoresPriorBinaryOnFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "plugin")
	if err := os.WriteFile(dest, []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	missingTemp := filepath.Join(dir, "does-not-exist")

	err := atomicReplace(missingTemp, dest)
	if err == nil {
		t.Fatal("expected atomicReplace to fail when the staged file is missing")
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("expected the prior binary to still be present, got read error: %v", readErr)
	}
	if string(got) != "old-binary" {
		t.Errorf("prior binary was not restored: dest content = %q, want %q", got, "old-binary")
	}
	if _, err := os.Stat(dest + ".bak"); !os.IsNotExist(err) {
		t.Error("expected no leftover .bak file after a successful restore")
	}
}

// ---------- checksums manifest parsing ----------

func TestParseChecksumsManifest(t *testing.T) {
	manifest := "deadbeef  myplugin_linux_amd64.tar.gz\n" +
		"cafebabe  myplugin_darwin_arm64.tar.gz\n" +
		"feedface *myplugin_windows_amd64.zip\n"

	got, err := parseChecksumsManifest(strings.NewReader(manifest), "myplugin_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "deadbeef" {
		t.Errorf("got %q, want %q", got, "deadbeef")
	}

	got, err = parseChecksumsManifest(strings.NewReader(manifest), "myplugin_windows_amd64.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got != "feedface" {
		t.Errorf("got %q, want %q (leading '*' must be stripped)", got, "feedface")
	}

	got, err = parseChecksumsManifest(strings.NewReader(manifest), "unknown.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for unknown asset", got)
	}
}

// ---------- Install (registry path) integration tests ----------

// newTestRegistryAndDownloadServer serves both the registry API JSON and the
// binary download from a single httptest server, and returns a *registry.Client
// pointed at it. binaryContent is streamed as the plugin binary; checksum, if
// non-empty, is embedded in the platform metadata.
func newTestRegistryAndDownloadServer(t *testing.T, binaryContent []byte, checksum string) (*registry.Client, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/download/plugin-binary", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	})

	var srv *httptest.Server
	mux.HandleFunc("/api/v1/extensions/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]registry.VersionInfo{{Version: "1.0.0"}})
		case strings.Contains(r.URL.Path, "/versions/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(registry.VersionDetail{
				VersionInfo: registry.VersionInfo{Version: "1.0.0"},
				Platforms: []registry.Platform{
					{
						OS:       runtime.GOOS,
						Arch:     runtime.GOARCH,
						URL:      srv.URL + "/download/plugin-binary",
						Checksum: checksum,
						Size:     int64(len(binaryContent)),
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	})

	srv = httptest.NewServer(mux)
	client := registry.NewClient(srv.URL, "")
	return client, srv.Close
}

func withPluginsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := pluginsDirFn
	pluginsDirFn = func() string { return dir }
	t.Cleanup(func() { pluginsDirFn = orig })
	return dir
}

func TestInstall_RegistryPath_Success(t *testing.T) {
	dir := withPluginsDir(t)

	content := []byte("fake-plugin-binary-content")
	sum := sha256.Sum256(content)
	client, closeSrv := newTestRegistryAndDownloadServer(t, content, hex.EncodeToString(sum[:]))
	defer closeSrv()

	dest, err := Install(context.Background(), client, "myplugin", "")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("installed content = %q, want %q", got, content)
	}

	// No staging temp files should be left behind in pluginsDir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tau-plugin-") {
			t.Errorf("staging temp file leaked: %s", e.Name())
		}
	}
}

func TestInstall_ChecksumMismatch_LeavesPriorBinaryIntact(t *testing.T) {
	dir := withPluginsDir(t)

	// Pre-install a plugin binary that must survive a failed update.
	dest := filepath.Join(dir, binName("myplugin"))
	if err := os.WriteFile(dest, []byte("original-good-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	content := []byte("corrupted-binary")
	client, closeSrv := newTestRegistryAndDownloadServer(t, content, "0000000000000000000000000000000000000000000000000000000000000000")
	defer closeSrv()

	_, err := Install(context.Background(), client, "myplugin", "")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("expected the previously installed binary to remain, got: %v", readErr)
	}
	if string(got) != "original-good-binary" {
		t.Errorf("prior binary was corrupted: got %q", got)
	}
}

func TestInstall_SizeLimitEnforced(t *testing.T) {
	withPluginsDir(t)

	origLimit := MaxDownloadSize
	MaxDownloadSize = 8 // bytes - far smaller than the payload below
	t.Cleanup(func() { MaxDownloadSize = origLimit })

	content := []byte("this payload is definitely bigger than 8 bytes")
	client, closeSrv := newTestRegistryAndDownloadServer(t, content, "")
	defer closeSrv()

	_, err := Install(context.Background(), client, "myplugin", "")
	if err == nil {
		t.Fatal("expected a size-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds size limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInstall_RejectsUnsafeID(t *testing.T) {
	withPluginsDir(t)
	client := registry.NewClient("http://unused.invalid", "")

	_, err := Install(context.Background(), client, "../escape", "")
	if err == nil {
		t.Fatal("expected Install to reject a path-traversal id before making any network call")
	}
}

// ---------- InstallFromGitHub integration tests ----------

// fakeHTTPDoer routes requests to handlers keyed by exact URL, for tests
// that need to fake GitHub's API + asset download without a real network
// call (GitHub API URLs are hardcoded, so httptest can't intercept them via
// baseURL injection the way the registry client can).
type fakeHTTPDoer struct {
	handlers map[string]func(*http.Request) (*http.Response, error)
}

func (f *fakeHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	h, ok := f.handlers[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("fakeHTTPDoer: no handler for %s", req.URL.String())
	}
	return h(req)
}

func jsonResponse(v any) (*http.Response, error) {
	data, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(data))),
		Header:     make(http.Header),
	}, nil
}

func bytesResponse(b []byte) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Header:     make(http.Header),
	}, nil
}

func withFakeHTTPClient(t *testing.T, f *fakeHTTPDoer) {
	t.Helper()
	orig := httpClient
	httpClient = f
	t.Cleanup(func() { httpClient = orig })
}

func TestInstallFromGitHub_VerifiesAgainstChecksumsManifest(t *testing.T) {
	withPluginsDir(t)

	binContent := []byte("fake-raw-binary")
	sum := sha256.Sum256(binContent)

	releaseURL := "https://api.github.com/repos/acme/widgets/releases/latest"
	assetName := fmt.Sprintf("myplugin_%s_%s", runtime.GOOS, runtime.GOARCH)
	checksumsManifest := hex.EncodeToString(sum[:]) + "  " + assetName + "\n"

	f := &fakeHTTPDoer{handlers: map[string]func(*http.Request) (*http.Response, error){
		releaseURL: func(r *http.Request) (*http.Response, error) {
			return jsonResponse(githubRelease{
				TagName: "v1.0.0",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: "https://example.invalid/" + assetName},
					{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums.txt"},
				},
			})
		},
		"https://example.invalid/checksums.txt": func(r *http.Request) (*http.Response, error) {
			return bytesResponse([]byte(checksumsManifest))
		},
		"https://example.invalid/" + assetName: func(r *http.Request) (*http.Response, error) {
			return bytesResponse(binContent)
		},
	}}
	withFakeHTTPClient(t, f)

	dest, err := InstallFromGitHub(context.Background(), "acme", "widgets", "myplugin", "")
	if err != nil {
		t.Fatalf("InstallFromGitHub failed: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binContent) {
		t.Errorf("installed content = %q, want %q", got, binContent)
	}
}

func TestInstallFromGitHub_ChecksumMismatchRejected(t *testing.T) {
	withPluginsDir(t)

	binContent := []byte("fake-raw-binary")
	releaseURL := "https://api.github.com/repos/acme/widgets/releases/latest"
	assetName := fmt.Sprintf("myplugin_%s_%s", runtime.GOOS, runtime.GOARCH)
	checksumsManifest := "0000000000000000000000000000000000000000000000000000000000000000  " + assetName + "\n"

	f := &fakeHTTPDoer{handlers: map[string]func(*http.Request) (*http.Response, error){
		releaseURL: func(r *http.Request) (*http.Response, error) {
			return jsonResponse(githubRelease{
				TagName: "v1.0.0",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: "https://example.invalid/" + assetName},
					{Name: "checksums.txt", BrowserDownloadURL: "https://example.invalid/checksums.txt"},
				},
			})
		},
		"https://example.invalid/checksums.txt": func(r *http.Request) (*http.Response, error) {
			return bytesResponse([]byte(checksumsManifest))
		},
		"https://example.invalid/" + assetName: func(r *http.Request) (*http.Response, error) {
			return bytesResponse(binContent)
		},
	}}
	withFakeHTTPClient(t, f)

	_, err := InstallFromGitHub(context.Background(), "acme", "widgets", "myplugin", "")
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInstallFromGitHub_RejectsUnsafePluginName(t *testing.T) {
	withPluginsDir(t)
	withFakeHTTPClient(t, &fakeHTTPDoer{handlers: map[string]func(*http.Request) (*http.Response, error){}})

	_, err := InstallFromGitHub(context.Background(), "acme", "widgets", "../escape", "")
	if err == nil {
		t.Fatal("expected InstallFromGitHub to reject a path-traversal plugin name before any network call")
	}
}

func TestInstallFromGitHub_NoManifestInstallsUnverified(t *testing.T) {
	withPluginsDir(t)

	binContent := []byte("fake-raw-binary")
	releaseURL := "https://api.github.com/repos/acme/widgets/releases/latest"
	assetName := fmt.Sprintf("myplugin_%s_%s", runtime.GOOS, runtime.GOARCH)

	f := &fakeHTTPDoer{handlers: map[string]func(*http.Request) (*http.Response, error){
		releaseURL: func(r *http.Request) (*http.Response, error) {
			return jsonResponse(githubRelease{
				TagName: "v1.0.0",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: "https://example.invalid/" + assetName},
				},
			})
		},
		"https://example.invalid/" + assetName: func(r *http.Request) (*http.Response, error) {
			return bytesResponse(binContent)
		},
	}}
	withFakeHTTPClient(t, f)

	dest, err := InstallFromGitHub(context.Background(), "acme", "widgets", "myplugin", "")
	if err != nil {
		t.Fatalf("expected install to succeed without a checksums manifest, got: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binContent) {
		t.Errorf("installed content = %q, want %q", got, binContent)
	}
}
