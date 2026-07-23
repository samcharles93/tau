package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	DefaultRepo       = "samcharles93/tau"
	defaultAPIBaseURL = "https://api.github.com"
	userAgent         = "tau"
)

var (
	ErrNoUpdate = errors.New("tau is already up to date")
	ErrDevBuild = errors.New("dev builds cannot update; use a release build for update checks")
)

type Options struct {
	CurrentVersion      string
	TargetVersion       string
	Repo                string
	CheckOnly           bool
	Force               bool
	TargetPath          string
	GOOS                string
	GOARCH              string
	HTTPClient          *http.Client
	APIBaseURL          string
	VerifyBinaryTimeout time.Duration
}

type Result struct {
	CurrentVersion string
	TargetVersion  string
	AssetName      string
	DownloadURL    string
	ReleaseURL     string
	Updated        bool
}

type release struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func Run(ctx context.Context, opts Options) (Result, error) {
	opts = withDefaults(opts)
	current := currentTag(opts.CurrentVersion)
	if current == "dev" {
		return Result{CurrentVersion: current}, ErrDevBuild
	}
	rel, err := fetchRelease(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		CurrentVersion: current,
		TargetVersion:  rel.TagName,
		ReleaseURL:     rel.HTMLURL,
	}
	if current == rel.TagName && !opts.Force {
		return result, ErrNoUpdate
	}

	assetName, err := ArchiveName(rel.TagName, opts.GOOS, opts.GOARCH)
	if err != nil {
		return Result{}, err
	}
	asset, ok := findAsset(rel.Assets, assetName)
	if !ok {
		return Result{}, fmt.Errorf("release %s has no asset for %s/%s (%s)", rel.TagName, opts.GOOS, opts.GOARCH, assetName)
	}
	result.AssetName = asset.Name
	result.DownloadURL = asset.BrowserDownloadURL

	if opts.CheckOnly {
		return result, nil
	}

	checksumAsset, ok := findAsset(rel.Assets, "checksums.txt")
	if !ok {
		return Result{}, fmt.Errorf("release %s has no checksums.txt asset", rel.TagName)
	}

	archiveBytes, err := fetchBytes(ctx, opts, asset.BrowserDownloadURL)
	if err != nil {
		return Result{}, fmt.Errorf("downloading %s: %w", asset.Name, err)
	}
	checksums, err := fetchBytes(ctx, opts, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return Result{}, fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyArchiveChecksum(asset.Name, archiveBytes, checksums); err != nil {
		return Result{}, err
	}

	binary, err := extractBinary(asset.Name, archiveBytes)
	if err != nil {
		return Result{}, err
	}
	if err := verifyBinaryRuns(binary, opts.GOOS, opts.VerifyBinaryTimeout); err != nil {
		return Result{}, err
	}
	binaryHash := sha256.Sum256(binary)
	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{
		TargetPath: opts.TargetPath,
		Checksum:   binaryHash[:],
	}); err != nil {
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return Result{}, fmt.Errorf("applying update: %w; rollback failed: %w", err, rollbackErr)
		}
		return Result{}, fmt.Errorf("applying update: %w", err)
	}
	result.Updated = true
	return result, nil
}

func withDefaults(opts Options) Options {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = defaultAPIBaseURL
	}
	if opts.VerifyBinaryTimeout == 0 {
		opts.VerifyBinaryTimeout = 10 * time.Second
	}
	return opts
}

func currentTag(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if token, _, ok := strings.Cut(version, " "); ok {
		return token
	}
	return version
}

func fetchRelease(ctx context.Context, opts Options) (release, error) {
	repo := strings.Trim(opts.Repo, "/")
	var endpoint string
	if opts.TargetVersion == "" {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(opts.APIBaseURL, "/"), repo)
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/tags/%s", strings.TrimRight(opts.APIBaseURL, "/"), repo, opts.TargetVersion)
	}

	var rel release
	if err := getJSON(ctx, opts, endpoint, &rel); err != nil {
		return release{}, err
	}
	if rel.TagName == "" {
		return release{}, errors.New("release response did not include tag_name")
	}
	return rel, nil
}

func getJSON(ctx context.Context, opts Options, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("github release request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func fetchBytes(ctx context.Context, opts Options, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("download failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func verifyArchiveChecksum(assetName string, archiveBytes, checksums []byte) error {
	expected, ok := checksumForAsset(assetName, checksums)
	if !ok {
		return fmt.Errorf("checksums.txt does not include %s", assetName)
	}
	actual := sha256.Sum256(archiveBytes)
	if !bytes.Equal(actual[:], expected) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func checksumForAsset(assetName string, checksums []byte) ([]byte, bool) {
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != assetName {
			continue
		}
		sum, err := hex.DecodeString(fields[0])
		if err != nil || len(sum) != sha256.Size {
			return nil, false
		}
		return sum, true
	}
	return nil, false
}

func extractBinary(assetName string, archiveBytes []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(assetName, ".tar.gz"):
		return extractTarGz(archiveBytes)
	case strings.HasSuffix(assetName, ".zip"):
		return extractZip(archiveBytes)
	default:
		return nil, fmt.Errorf("unsupported release archive %s", assetName)
	}
}

func extractTarGz(archiveBytes []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg || !isTauBinary(header.Name) {
			continue
		}
		return io.ReadAll(tr)
	}
	return nil, errors.New("release archive did not contain tau binary")
}

func extractZip(archiveBytes []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.FileInfo().IsDir() || !isTauBinary(file.Name) {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		binary, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return binary, nil
	}
	return nil, errors.New("release archive did not contain tau binary")
}

func verifyBinaryRuns(binary []byte, goos string, timeout time.Duration) error {
	name := "tau"
	if goos == "windows" {
		name = "tau.exe"
	}
	f, err := os.CreateTemp("", name+"-verify-*")
	if err != nil {
		return fmt.Errorf("creating temp file for binary verification: %w", err)
	}
	if _, err := f.Write(binary); err != nil {
		f.Close()
		os.Remove(f.Name())
		return fmt.Errorf("writing temp binary for verification: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("closing temp binary for verification: %w", err)
	}
	defer os.Remove(f.Name())

	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return fmt.Errorf("making temp binary executable: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, f.Name(), "--version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("downloaded binary did not respond to --version within %v — leaving existing install untouched", timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("downloaded binary failed --version check: exit status %d — leaving existing install untouched", exitErr.ExitCode())
		}
		return fmt.Errorf("downloaded binary failed --version check: %w — leaving existing install untouched", err)
	}
	return nil
}

func isTauBinary(name string) bool {
	base := filepath.Base(name)
	return base == "tau" || base == "tau.exe"
}
