package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupportedTargets(t *testing.T) {
	t.Parallel()

	targets := SupportedTargets()
	require.ElementsMatch(t, []Target{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
	}, targets, "must match .goreleaser.yaml's builds.goos/goarch matrix")
}

func TestArchiveName(t *testing.T) {
	t.Parallel()

	// Every supported target produces the expected archive name, with the
	// right extension per OS.
	for _, target := range SupportedTargets() {
		name, err := ArchiveName("v1.2.3", target.OS, target.Arch)
		require.NoError(t, err)
		wantExt := ".tar.gz"
		if target.OS == "windows" {
			wantExt = ".zip"
		}
		require.Equal(t, fmt.Sprintf("tau_1.2.3_%s_%s%s", target.OS, target.Arch, wantExt), name)
	}

	_, err := ArchiveName("", "linux", "amd64")
	require.Error(t, err, "empty tag must be rejected")

	_, err = ArchiveName("v1.2.3", "plan9", "amd64")
	require.ErrorContains(t, err, "unsupported release target", "unsupported target must return a clear error")
}

func TestChecksumForAsset(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte("archive"))
	checksums := fmt.Appendf(nil, "%x  tau_1.2.3_linux_amd64.tar.gz\n", sum)

	got, ok := checksumForAsset("tau_1.2.3_linux_amd64.tar.gz", checksums)
	require.True(t, ok)
	require.Equal(t, sum[:], got)
}

func TestExtractBinary(t *testing.T) {
	t.Parallel()

	goodBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Stdout.WriteString("test v1.0.0\n")
		os.Exit(0)
	}
	os.Exit(1)
}`)

	tarGz := makeTarGz(t, "tau_1.2.3_linux_amd64/tau", goodBin)
	got, err := extractBinary("tau_1.2.3_linux_amd64.tar.gz", tarGz)
	require.NoError(t, err)
	require.Equal(t, goodBin, got)

	zipBytes := makeZip(t, "tau_1.2.3_windows_amd64/tau.exe", goodBin)
	got, err = extractBinary("tau_1.2.3_windows_amd64.zip", zipBytes)
	require.NoError(t, err)
	require.Equal(t, goodBin, got)
}

func TestRunCheckOnlyDoesNotApplyUpdate(t *testing.T) {
	t.Parallel()

	goodBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Stdout.WriteString("test v1.0.0\n")
		os.Exit(0)
	}
	os.Exit(1)
}`)

	server := fakeReleaseServer(t, goodBin)
	targetPath := filepath.Join(t.TempDir(), "tau")
	require.NoError(t, os.WriteFile(targetPath, []byte("old-binary"), 0o755))

	result, err := Run(context.Background(), Options{
		CurrentVersion:      "v1.0.0 (abc, date)",
		Repo:                "samcharles93/tau",
		CheckOnly:           true,
		TargetPath:          targetPath,
		GOOS:                "linux",
		GOARCH:              "amd64",
		HTTPClient:          server.Client(),
		APIBaseURL:          server.URL,
		VerifyBinaryTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.False(t, result.Updated)
	require.Equal(t, "v1.2.3", result.TargetVersion)

	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("old-binary"), targetBytes)
}

func TestRunAppliesUpdate(t *testing.T) {
	t.Parallel()

	goodBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Stdout.WriteString("test v1.0.0\n")
		os.Exit(0)
	}
	os.Exit(1)
}`)

	server := fakeReleaseServer(t, goodBin)
	targetPath := filepath.Join(t.TempDir(), "tau")
	require.NoError(t, os.WriteFile(targetPath, []byte("old-binary"), 0o755))

	result, err := Run(context.Background(), Options{
		CurrentVersion:      "v1.0.0 (abc, date)",
		Repo:                "samcharles93/tau",
		TargetPath:          targetPath,
		GOOS:                "linux",
		GOARCH:              "amd64",
		HTTPClient:          server.Client(),
		APIBaseURL:          server.URL,
		VerifyBinaryTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	require.True(t, result.Updated)
	require.Equal(t, "tau_1.2.3_linux_amd64.tar.gz", result.AssetName)

	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, goodBin, targetBytes)
}

func TestRunNoUpdate(t *testing.T) {
	t.Parallel()

	server := fakeReleaseServer(t, []byte("new-binary"))
	_, err := Run(context.Background(), Options{
		CurrentVersion: "v1.2.3 (abc, date)",
		Repo:           "samcharles93/tau",
		GOOS:           "linux",
		GOARCH:         "amd64",
		HTTPClient:     server.Client(),
		APIBaseURL:     server.URL,
	})
	require.ErrorIs(t, err, ErrNoUpdate)
}

func TestRunDevBuildReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "dev",
		Repo:           "samcharles93/tau",
	})
	require.ErrorIs(t, err, ErrDevBuild)
}

func TestRunDevBuildReturnsErrorEvenWithForce(t *testing.T) {
	t.Parallel()

	_, err := Run(context.Background(), Options{
		CurrentVersion: "dev",
		Repo:           "samcharles93/tau",
		Force:          true,
	})
	require.ErrorIs(t, err, ErrDevBuild)
}

func TestCurrentTag(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1.2.3", currentTag("v1.2.3 (abc, 2026-07-10)"))
	require.Equal(t, "dev", currentTag("dev"))
	require.Equal(t, "", currentTag(strings.TrimSpace(" ")))
}

// --- verifyBinaryRuns tests ---

func TestVerifyBinaryRunsSuccess(t *testing.T) {
	t.Parallel()

	goodBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Stdout.WriteString("test v1.0.0\n")
		os.Exit(0)
	}
	os.Exit(1)
}`)

	err := verifyBinaryRuns(goodBin, "linux", 5*time.Second)
	require.NoError(t, err)
}

func TestVerifyBinaryRunsNonZero(t *testing.T) {
	t.Parallel()

	badBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Stderr.WriteString("error: something went wrong\n")
		os.Exit(2)
	}
	os.Exit(0)
}`)

	err := verifyBinaryRuns(badBin, "linux", 5*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed --version check: exit status 2")
	require.Contains(t, err.Error(), "leaving existing install untouched")
}

func TestVerifyBinaryRunsHang(t *testing.T) {
	t.Parallel()

	hangBin := buildTestBinary(t, `package main
import (
	"os"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	os.Exit(0)
}`)

	err := verifyBinaryRuns(hangBin, "linux", 1*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "did not respond to --version within")
	require.Contains(t, err.Error(), "leaving existing install untouched")
}

// --- end-to-end: corrupt binary leaves existing install untouched ---

func TestRunRejectsBinaryThatFailsVersionCheck(t *testing.T) {
	t.Parallel()

	badBin := buildTestBinary(t, `package main
import "os"
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Exit(1)
	}
	os.Exit(0)
}`)

	server := fakeReleaseServer(t, badBin)
	targetPath := filepath.Join(t.TempDir(), "tau")
	existing := []byte("old-working-binary")
	require.NoError(t, os.WriteFile(targetPath, existing, 0o755))

	_, err := Run(context.Background(), Options{
		CurrentVersion:      "v1.0.0 (abc, date)",
		Repo:                "samcharles93/tau",
		TargetPath:          targetPath,
		GOOS:                "linux",
		GOARCH:              "amd64",
		HTTPClient:          server.Client(),
		APIBaseURL:          server.URL,
		VerifyBinaryTimeout: 5 * time.Second,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed --version check")
	require.Contains(t, err.Error(), "leaving existing install untouched")

	// Existing binary must be untouched.
	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, existing, targetBytes)
}

// --- helpers ---

func fakeReleaseServer(t *testing.T, binary []byte) *httptest.Server {
	t.Helper()

	archive := makeTarGz(t, "tau_1.2.3_linux_amd64/tau", binary)
	archiveSum := sha256.Sum256(archive)
	checksums := fmt.Appendf(nil, "%x  tau_1.2.3_linux_amd64.tar.gz\n", archiveSum)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/repos/samcharles93/tau/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": "v1.2.3",
			"html_url": "%s/releases/tag/v1.2.3",
			"assets": [
				{"name": "tau_1.2.3_linux_amd64.tar.gz", "browser_download_url": "%s/download/tau_1.2.3_linux_amd64.tar.gz"},
				{"name": "checksums.txt", "browser_download_url": "%s/download/checksums.txt"}
			]
		}`, server.URL, server.URL, server.URL)
	})
	mux.HandleFunc("/download/tau_1.2.3_linux_amd64.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(checksums)
	})
	return server
}

func buildTestBinary(t *testing.T, mainSrc string) []byte {
	t.Helper()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(srcPath, []byte(mainSrc), 0o644))
	outPath := filepath.Join(dir, "testbin")

	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", outPath, srcPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	bin, err := os.ReadFile(outPath)
	require.NoError(t, err)
	return bin
}

func makeTarGz(t *testing.T, name string, contents []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(contents)),
	}))
	_, err := tw.Write(contents)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, contents []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	require.NoError(t, err)
	_, err = w.Write(contents)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
