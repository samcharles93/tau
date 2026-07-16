package plugin

import (
	"archive/tar"
	"archive/zip"
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
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/plugin/registry"
)

// InstalledPlugin represents a plugin binary found on disk.
type InstalledPlugin struct {
	Name string
	Size int64
}

// Install downloads a plugin binary from the registry and places it in the
// plugins directory. If version is empty, the latest version is resolved.
// Returns the path to the installed binary.
func Install(ctx context.Context, client *registry.Client, id, version string) (string, error) {
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

	// Download binary.
	fmt.Printf("  downloading from %s\n", plat.URL)
	tmp, err := os.CreateTemp("", "tau-plugin-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	req, err := http.NewRequestWithContext(ctx, "GET", plat.URL, nil)
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("create download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body)
	tmp.Close()
	if err != nil {
		return "", fmt.Errorf("save binary: %w", err)
	}

	// Verify checksum if provided.
	if plat.Checksum != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != plat.Checksum {
			return "", fmt.Errorf("checksum mismatch: got %s, want %s", got[:16]+"...", plat.Checksum[:16]+"...")
		}
		fmt.Printf("  ✓ verified (SHA256: %s...)\n", got[:16])
	} else if written != plat.Size && plat.Size > 0 {
		return "", fmt.Errorf("size mismatch: got %d, expected %d", written, plat.Size)
	}

	// Place in plugins directory.
	pluginsDir := pluginsDir()
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugins dir: %w", err)
	}

	dest := filepath.Join(pluginsDir, id)
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o755); err != nil {
			return "", fmt.Errorf("chmod binary: %w", err)
		}
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
	pluginsDir := pluginsDir()
	dest := filepath.Join(pluginsDir, id)
	if runtime.GOOS == "windows" {
		dest += ".exe"
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
		if entry.Name() == ".gitignore" {
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
	pluginsDir := pluginsDir()
	dest := filepath.Join(pluginsDir, id)
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	_, err := os.Stat(dest)
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

// InstallFromGitHub downloads a plugin binary directly from a GitHub release.
// source is in "owner/repo:plugin[@version]" format. If version is empty,
// the latest release is used.
func InstallFromGitHub(ctx context.Context, owner, repo, plugin, version string) (string, error) {
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

	fmt.Printf("  downloading from %s\n", asset.BrowserDownloadURL)

	tmpDir, err := os.MkdirTemp("", "tau-plugin-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, asset.Name)
	if err := downloadFile(ctx, asset.BrowserDownloadURL, archivePath); err != nil {
		return "", err
	}

	// Extract the binary from the archive.
	binaryPath, err := extractBinary(archivePath, plugin)
	if err != nil {
		return "", fmt.Errorf("extract binary: %w", err)
	}

	// Place in plugins directory.
	pluginsDir := pluginsDir()
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugins dir: %w", err)
	}

	dest := filepath.Join(pluginsDir, plugin)
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	if err := os.Rename(binaryPath, dest); err != nil {
		return "", fmt.Errorf("install binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o755); err != nil {
			return "", fmt.Errorf("chmod binary: %w", err)
		}
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

func fetchGitHubRelease(ctx context.Context, owner, repo, version string) (*githubRelease, error) {
	var url string
	if version == "" || version == "latest" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/v%s", owner, repo, version)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &release, nil
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

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func extractBinary(archivePath, pluginName string) (string, error) {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return extractTarGz(archivePath, pluginName)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, pluginName)
	default:
		// Raw binary - rename to plugin name.
		return archivePath, nil
	}
}

func extractTarGz(path, pluginName string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		base := filepath.Base(header.Name)
		if base == pluginName || base == pluginName+".exe" {
			outDir := filepath.Dir(path)
			outPath := filepath.Join(outDir, pluginName)
			out, err := os.Create(outPath)
			if err != nil {
				return "", err
			}
			defer out.Close()

			if _, err := io.Copy(out, tr); err != nil {
				return "", err
			}
			return outPath, nil
		}
	}
	return "", fmt.Errorf("binary %q not found in archive", pluginName)
}

func extractZip(path, pluginName string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base == pluginName || base == pluginName+".exe" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()

			outDir := filepath.Dir(path)
			outPath := filepath.Join(outDir, pluginName)
			out, err := os.Create(outPath)
			if err != nil {
				return "", err
			}
			defer out.Close()

			if _, err := io.Copy(out, rc); err != nil {
				return "", err
			}
			return outPath, nil
		}
	}
	return "", fmt.Errorf("binary %q not found in archive", pluginName)
}

// SentinelFile is the filename written to the config directory by CLI
// install/uninstall to signal a running tau process to reload plugins.
const SentinelFile = ".plugins_changed"

func pluginsDir() string {
	return filepath.Join(config.Dir(), "plugins")
}

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
