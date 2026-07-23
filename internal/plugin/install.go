package plugin

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/plugin/registry"
)

// httpDoer is the subset of *http.Client the installer depends on, so tests
// can inject a fake transport without a real network call.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpClient is used for every plugin download and GitHub API call. It is a
// package variable (not a hardcoded http.DefaultClient reference) so tests
// can swap in a fake httpDoer.
var httpClient httpDoer = http.DefaultClient

// pluginsDirFn resolves the plugins directory. It is a package variable so
// tests can point installs at a temp directory without touching the real
// config directory (TAU_CONFIG_DIR already does this for most tests, but a
// direct override keeps install tests independent of env state).
var pluginsDirFn = func() string {
	return filepath.Join(config.Dir(), "plugins")
}

func pluginsDir() string {
	return pluginsDirFn()
}

// InstalledPlugin represents a plugin binary found on disk.
type InstalledPlugin struct {
	Name string
	Size int64
}

// Install downloads a plugin binary from the registry and places it in the
// plugins directory. If version is empty, the latest version is resolved.
// Returns the path to the installed binary.
func Install(ctx context.Context, client *registry.Client, id, version string) (string, error) {
	dest, err := resolvePluginBinaryPath(pluginsDir(), id)
	if err != nil {
		return "", err
	}

	// Resolve version.
	if version == "" || version == "latest" {
		versions, err := client.ListVersions(ctx, id)
		if err != nil {
			return "", fmt.Errorf("list versions for %q: %w", id, err)
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no versions available for %q", id)
		}
		version = versions[0].Version
	}

	// Fetch version with platforms.
	ver, err := client.GetVersion(ctx, id, version)
	if err != nil {
		return "", fmt.Errorf("fetch version %s@%s: %w", id, version, err)
	}

	// Find matching platform.
	var plat *registry.Platform
	for i := range ver.Platforms {
		p := &ver.Platforms[i]
		if p.OS == runtime.GOOS && p.Arch == runtime.GOARCH {
			plat = p
			break
		}
	}
	if plat == nil {
		return "", fmt.Errorf("no binary for %s/%s in %s@%s", runtime.GOOS, runtime.GOARCH, id, version)
	}

	fmt.Printf("  downloading from %s\n", plat.URL)

	tmp, err := stagePluginTempFile(pluginsDir())
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", plat.URL, nil)
	if err != nil {
		discardStaged(tmp)
		return "", fmt.Errorf("create download request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		discardStaged(tmp)
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		discardStaged(tmp)
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	hasher := sha256.New()
	written, err := copyLimited(io.MultiWriter(tmp, hasher), resp.Body, MaxDownloadSize)
	if err != nil {
		discardStaged(tmp)
		return "", fmt.Errorf("save binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("save binary: %w", err)
	}

	// Verify checksum if provided.
	if plat.Checksum != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != plat.Checksum {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("checksum mismatch: got %s, want %s", got[:16]+"...", plat.Checksum[:16]+"...")
		}
		fmt.Printf("  ✓ verified (SHA256: %s...)\n", got[:16])
	} else if written != plat.Size && plat.Size > 0 {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("size mismatch: got %d, expected %d", written, plat.Size)
	}

	if err := atomicReplace(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}

	// Signal running tau to reload.
	if err := signalReload(); err != nil {
		// Non-fatal - user can restart tau.
		fmt.Printf("  ⚠ could not signal reload: %v\n", err)
	} else {
		fmt.Println("  ✓ plugins reloaded")
	}

	fmt.Printf("  ✓ installed to %s\n", dest)
	return dest, nil
}

// Uninstall removes a plugin binary from the plugins directory.
func Uninstall(id string) error {
	dest, err := resolvePluginBinaryPath(pluginsDir(), id)
	if err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin %q is not installed", id)
		}
		return fmt.Errorf("remove plugin: %w", err)
	}

	// Signal running tau to reload.
	if err := signalReload(); err != nil {
		// Non-fatal - user can restart tau.
		fmt.Printf("  ⚠ could not signal reload: %v\n", err)
	}
	return nil
}

// ListInstalled returns all plugin binaries found in the plugins directory.
func ListInstalled() ([]InstalledPlugin, error) {
	pluginsDir := pluginsDir()
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plugins dir: %w", err)
	}

	var installed []InstalledPlugin
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() == ".gitignore" || strings.HasPrefix(entry.Name(), ".tau-plugin-") || strings.HasSuffix(entry.Name(), ".bak") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		installed = append(installed, InstalledPlugin{
			Name: entry.Name(),
			Size: info.Size(),
		})
	}
	return installed, nil
}

// IsInstalled reports whether a plugin is present in the plugins directory.
func IsInstalled(id string) bool {
	dest, err := resolvePluginBinaryPath(pluginsDir(), id)
	if err != nil {
		return false
	}
	_, err = os.Stat(dest)
	return err == nil
}

// ---------- GitHub direct install ----------

// githubReleaseAsset represents a single asset in a GitHub release.
type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

// maxChecksumsFileSize bounds the checksums manifest download - it is a
// small text file, so a generous cap here is still a strict bound.
const maxChecksumsFileSize int64 = 1 * 1024 * 1024 // 1 MiB

// checksumsAssetNames are the conventional names goreleaser and similar
// tools publish alongside release binaries.
var checksumsAssetNames = []string{"checksums.txt", "checksums.sha256", "sha256sums.txt", "sha256sums"}

// InstallFromGitHub downloads a plugin binary directly from a GitHub release.
// source is in "owner/repo:plugin[@version]" format. If version is empty,
// the latest release is used.
//
// If the release publishes a conventional checksums manifest (checksums.txt
// or similar, as goreleaser and equivalents produce), the downloaded asset
// is verified against it before extraction/activation and a mismatch is
// fatal. GitHub releases have no mandated checksum mechanism, so when no
// manifest is published the install proceeds but is clearly logged as
// unverified rather than silently treated as trusted.
func InstallFromGitHub(ctx context.Context, owner, repo, plugin, version string) (string, error) {
	dest, err := resolvePluginBinaryPath(pluginsDir(), plugin)
	if err != nil {
		return "", err
	}

	release, err := fetchGitHubRelease(ctx, owner, repo, version)
	if err != nil {
		return "", err
	}

	// Find an asset matching the current OS/arch.
	asset := findPlatformAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if asset == nil {
		return "", fmt.Errorf("no %s/%s binary in %s/%s release %s",
			runtime.GOOS, runtime.GOARCH, owner, repo, release.TagName)
	}

	expectedChecksum, err := fetchReleaseChecksum(ctx, release, asset.Name)
	if err != nil {
		return "", fmt.Errorf("fetch release checksums: %w", err)
	}

	fmt.Printf("  downloading from %s\n", asset.BrowserDownloadURL)

	tmpDir, err := os.MkdirTemp("", "tau-plugin-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, asset.Name)
	sum, err := downloadFile(ctx, asset.BrowserDownloadURL, archivePath, MaxDownloadSize)
	if err != nil {
		return "", err
	}

	if expectedChecksum != "" {
		if sum != expectedChecksum {
			return "", fmt.Errorf("checksum mismatch for %s: got %s, want %s", asset.Name, sum, expectedChecksum)
		}
		fmt.Printf("  ✓ verified (SHA256: %s...)\n", sum[:16])
	} else {
		fmt.Println("  ⚠ release publishes no checksum manifest; binary integrity was not verified")
	}

	// Extract the binary from the archive directly into a pluginsDir-local
	// staging file, so activation below is a same-filesystem atomic rename
	// rather than a cross-filesystem copy from the OS temp directory.
	staged, err := stagePluginTempFile(pluginsDir())
	if err != nil {
		return "", err
	}
	if err := extractBinaryTo(archivePath, plugin, staged); err != nil {
		discardStaged(staged)
		return "", fmt.Errorf("extract binary: %w", err)
	}
	if err := staged.Close(); err != nil {
		os.Remove(staged.Name())
		return "", fmt.Errorf("extract binary: %w", err)
	}

	if err := atomicReplace(staged.Name(), dest); err != nil {
		os.Remove(staged.Name())
		return "", err
	}

	// Signal running tau to reload.
	if err := signalReload(); err != nil {
		fmt.Printf("  ⚠ could not signal reload: %v\n", err)
	} else {
		fmt.Println("  ✓ plugins reloaded")
	}

	fmt.Printf("  ✓ installed to %s\n", dest)
	return dest, nil
}

// githubAPIBase is the GitHub REST API root. It is a package variable (not
// a hardcoded literal in fetchGitHubRelease) so tests can point it at a
// local httptest server instead of making a real network call.
var githubAPIBase = "https://api.github.com"

func fetchGitHubRelease(ctx context.Context, owner, repo, version string) (*githubRelease, error) {
	var url string
	if version == "" || version == "latest" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, owner, repo)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, owner, repo, normalizeReleaseTag(version))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release not found for %s/%s", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxChecksumsFileSize)).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &release, nil
}

// fetchReleaseChecksum looks for a conventional checksums manifest among the
// release assets and, if found, returns the expected hex SHA256 digest for
// assetName. Returns "" (not an error) if no manifest is published.
func fetchReleaseChecksum(ctx context.Context, release *githubRelease, assetName string) (string, error) {
	var manifest *githubReleaseAsset
	for i := range release.Assets {
		name := strings.ToLower(release.Assets[i].Name)
		if slicesContainsFold(checksumsAssetNames, name) {
			manifest = &release.Assets[i]
			break
		}
	}
	if manifest == nil {
		return "", nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", manifest.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	return parseChecksumsManifest(io.LimitReader(resp.Body, maxChecksumsFileSize), assetName)
}

func slicesContainsFold(candidates []string, name string) bool {
	return slices.Contains(candidates, name)
}

// parseChecksumsManifest parses a "<hexdigest>  <filename>" per-line
// manifest (the format goreleaser and sha256sum both produce) and returns
// the digest for filename, or "" if not present.
func parseChecksumsManifest(r io.Reader, filename string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		digest := strings.ToLower(fields[0])
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) == filename {
			return digest, nil
		}
	}
	return "", scanner.Err()
}

// normalizeReleaseTag maps a user-supplied version to the GitHub release
// tag GitHub actually uses. The CLI documents and accepts both
// "owner/repo:plugin@1.2.0" and "owner/repo:plugin@v1.2.0", but release
// tags are conventionally v-prefixed, so the "v" must be applied exactly
// once regardless of whether the caller already included it.
func normalizeReleaseTag(version string) string {
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return "v" + version[1:]
	}
	return "v" + version
}

func findPlatformAsset(assets []githubReleaseAsset, goos, goarch string) *githubReleaseAsset {
	// Preferred patterns (most specific first).
	candidates := []string{
		fmt.Sprintf("_%s_%s.tar.gz", goos, goarch),
		fmt.Sprintf("_%s_%s.zip", goos, goarch),
		fmt.Sprintf("-%s-%s.tar.gz", goos, goarch),
		fmt.Sprintf("-%s-%s.zip", goos, goarch),
		fmt.Sprintf("_%s_%s", goos, goarch),
	}

	for _, pattern := range candidates {
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if strings.Contains(name, pattern) {
				return &assets[i]
			}
		}
	}
	return nil
}

// downloadFile downloads url to dest, enforcing limit bytes, and returns the
// hex SHA256 digest of what was written.
func downloadFile(ctx context.Context, url, dest string, limit int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := copyLimited(io.MultiWriter(f, hasher), resp.Body, limit); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// extractBinaryTo extracts pluginName's binary from the archive at
// archivePath directly into out, so callers can stage it wherever they need
// (e.g. inside pluginsDir for an atomic same-filesystem activation) without
// an extra copy.
func extractBinaryTo(archivePath, pluginName string, out io.Writer) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return extractTarGzTo(archivePath, pluginName, out)
	case strings.HasSuffix(lower, ".zip"):
		return extractZipTo(archivePath, pluginName, out)
	default:
		// Raw binary.
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(out, f)
		return err
	}
}

func extractTarGzTo(path, pluginName string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		base := filepath.Base(header.Name)
		if base == pluginName || base == pluginName+".exe" {
			_, err := io.Copy(out, tr)
			return err
		}
	}
	return fmt.Errorf("binary %q not found in archive", pluginName)
}

func extractZipTo(path, pluginName string, out io.Writer) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base == pluginName || base == pluginName+".exe" {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("binary %q not found in archive", pluginName)
}

// SentinelFile is the filename written to the config directory by CLI
// install/uninstall to signal a running tau process to reload plugins.
const SentinelFile = ".plugins_changed"

// signalReload writes a sentinel file so a running tau process picks up
// plugin changes on its next schedule tick.
func signalReload() error {
	sentinel := filepath.Join(config.Dir(), SentinelFile)
	f, err := os.Create(sentinel)
	if err != nil {
		return err
	}
	return f.Close()
}
