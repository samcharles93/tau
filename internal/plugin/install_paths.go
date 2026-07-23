package plugin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// DefaultMaxDownloadSize bounds any single plugin download (registry binary
// or GitHub release asset) so a malicious or misconfigured server cannot
// exhaust local disk space before a size/checksum check ever runs.
const DefaultMaxDownloadSize int64 = 256 * 1024 * 1024 // 256 MiB

// MaxDownloadSize is the effective download size limit. It is a package
// variable (rather than a constant) so it is configurable - e.g. from
// config.yaml or an environment variable at startup - and so tests can
// exercise the oversized-payload path without downloading 256 MiB.
var MaxDownloadSize = DefaultMaxDownloadSize

// pluginNamePattern matches safe plugin identifiers: must start with a
// letter or digit (which already excludes "", ".", and "..", all of which
// start with something other than an interior-safe character or are empty)
// and contain only letters, digits, '.', '_', or '-'. Path separators
// ('/', '\'), volume/drive qualifiers (':'), and any other punctuation are
// rejected outright, so a name can never smuggle a directory traversal or
// an absolute/UNC/volume-qualified path.
var pluginNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidatePluginName rejects any plugin identifier that isn't a plain,
// single path segment safe to join under pluginsDir. It is exported so
// callers outside this package (e.g. CLI source-spec parsing) can reject
// unsafe plugin identifiers before they ever reach the installer.
func ValidatePluginName(name string) error {
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}
	if !pluginNamePattern.MatchString(name) {
		return fmt.Errorf("invalid plugin name %q: must start with a letter or digit and contain only letters, digits, '.', '_', or '-'", name)
	}
	return nil
}

// resolvePluginBinaryPath validates name and returns the absolute path
// where its binary belongs inside pluginsDir, including the platform's
// executable suffix. It re-checks (defense in depth, beyond the character
// allowlist above) that the resolved path is actually still inside
// pluginsDir before returning it.
func resolvePluginBinaryPath(pluginsDir, name string) (string, error) {
	if err := ValidatePluginName(name); err != nil {
		return "", err
	}

	dest := filepath.Join(pluginsDir, name)
	rel, err := filepath.Rel(pluginsDir, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid plugin name %q: resolves outside the plugins directory", name)
	}

	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	return dest, nil
}

// copyLimited copies src into dst, failing once more than limit bytes have
// been read. It always drains up to limit+1 bytes so callers get a clear
// "exceeds size limit" error instead of a truncated, silently-incomplete
// file being mistaken for a complete download.
func copyLimited(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("download exceeds size limit of %d bytes", limit)
	}
	return n, nil
}

// stagePluginTempFile creates a temp file inside pluginsDir (creating the
// directory if needed) so the final activation step is a same-filesystem,
// atomic rename rather than a cross-filesystem copy that can fail partway
// or silently degrade to a non-atomic copy+delete.
func stagePluginTempFile(pluginsDir string) (*os.File, error) {
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugins dir: %w", err)
	}
	f, err := os.CreateTemp(pluginsDir, ".tau-plugin-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create staging file: %w", err)
	}
	return f, nil
}

// atomicReplace activates a staged plugin binary at tempPath as destPath.
// On unix it sets the executable bit on the staged file before the rename
// (permissions must be correct before the file is visible under its final
// name); on Windows, where the executable bit is not meaningful, replacement
// alone is sufficient - Go's os.Rename uses MoveFileEx with
// MOVEFILE_REPLACE_EXISTING there, so it already atomically replaces an
// existing destination on the same volume.
//
// If a plugin is already installed at destPath, it is preserved: replaced
// only after the new binary is confirmed staged, and restored if the final
// rename fails for any reason, so a failed install/update never leaves the
// plugin directory without a working binary.
func atomicReplace(tempPath, destPath string) error {
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempPath, 0o755); err != nil {
			return fmt.Errorf("chmod staged binary: %w", err)
		}
	}

	backupPath := destPath + ".bak"
	_ = os.Remove(backupPath) // clear any stale backup from a previous failed install

	hadExisting := false
	if _, err := os.Stat(destPath); err == nil {
		hadExisting = true
		if err := os.Rename(destPath, backupPath); err != nil {
			return fmt.Errorf("back up existing plugin: %w", err)
		}
	}

	if err := os.Rename(tempPath, destPath); err != nil {
		if hadExisting {
			if restoreErr := os.Rename(backupPath, destPath); restoreErr != nil {
				return fmt.Errorf("replace plugin binary: %w (also failed to restore previous binary: %v)", err, restoreErr)
			}
		}
		return fmt.Errorf("replace plugin binary: %w", err)
	}

	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}

// discardStaged removes a staged temp file that will never be activated,
// e.g. because a checksum or size check failed after it was written.
func discardStaged(f *os.File) {
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
}
