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
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveName(t *testing.T) {
	t.Parallel()

	name, err := archiveName("v1.2.3", "linux", "amd64")
	require.NoError(t, err)
	require.Equal(t, "tau_1.2.3_linux_amd64.tar.gz", name)

	name, err = archiveName("v1.2.3", "windows", "arm64")
	require.NoError(t, err)
	require.Equal(t, "tau_1.2.3_windows_arm64.zip", name)

	name, err = archiveName("v1.2.3", "darwin", "amd64")
	require.NoError(t, err)
	require.Equal(t, "tau_1.2.3_darwin_amd64.tar.gz", name)

	name, err = archiveName("v1.2.3", "darwin", "arm64")
	require.NoError(t, err)
	require.Equal(t, "tau_1.2.3_darwin_arm64.tar.gz", name)

	_, err = archiveName("", "linux", "amd64")
	require.Error(t, err)
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

	tarGz := makeTarGz(t, "tau_1.2.3_linux_amd64/tau", []byte("new-binary"))
	got, err := extractBinary("tau_1.2.3_linux_amd64.tar.gz", tarGz)
	require.NoError(t, err)
	require.Equal(t, []byte("new-binary"), got)

	zipBytes := makeZip(t, "tau_1.2.3_windows_amd64/tau.exe", []byte("windows-binary"))
	got, err = extractBinary("tau_1.2.3_windows_amd64.zip", zipBytes)
	require.NoError(t, err)
	require.Equal(t, []byte("windows-binary"), got)
}

func TestRunCheckOnlyDoesNotApplyUpdate(t *testing.T) {
	t.Parallel()

	server := fakeReleaseServer(t, []byte("new-binary"))
	targetPath := filepath.Join(t.TempDir(), "tau")
	require.NoError(t, os.WriteFile(targetPath, []byte("old-binary"), 0o755))

	result, err := Run(context.Background(), Options{
		CurrentVersion: "v1.0.0 (abc, date)",
		Repo:           "samcharles93/tau",
		CheckOnly:      true,
		TargetPath:     targetPath,
		GOOS:           "linux",
		GOARCH:         "amd64",
		HTTPClient:     server.Client(),
		APIBaseURL:     server.URL,
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

	server := fakeReleaseServer(t, []byte("new-binary"))
	targetPath := filepath.Join(t.TempDir(), "tau")
	require.NoError(t, os.WriteFile(targetPath, []byte("old-binary"), 0o755))

	result, err := Run(context.Background(), Options{
		CurrentVersion: "v1.0.0 (abc, date)",
		Repo:           "samcharles93/tau",
		TargetPath:     targetPath,
		GOOS:           "linux",
		GOARCH:         "amd64",
		HTTPClient:     server.Client(),
		APIBaseURL:     server.URL,
	})
	require.NoError(t, err)
	require.True(t, result.Updated)
	require.Equal(t, "tau_1.2.3_linux_amd64.tar.gz", result.AssetName)

	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("new-binary"), targetBytes)
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

func TestCurrentTag(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1.2.3", currentTag("v1.2.3 (abc, 2026-07-10)"))
	require.Equal(t, "dev", currentTag("dev"))
	require.Equal(t, "", currentTag(strings.TrimSpace(" ")))
}
