package tools

import (
	"path/filepath"
	"strings"
)

const (
	// maxReadBytes is the maximum file size the read tool will load into memory.
	maxReadBytes = 5 * 1024 * 1024 // 5MB

	// maxWriteBytes is the maximum content size the write tool will accept.
	maxWriteBytes = 5 * 1024 * 1024 // 5MB
)

// resolvePath resolves a potentially relative path against the working directory.
// It also strips a leading @ (some LLMs include this).
func resolvePath(cwd, path string) string {
	path = strings.TrimPrefix(path, "@")
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

// isConfined checks whether target is within (or equal to) the base directory.
// Returns false if target escapes via ../ or is an unrelated absolute path.
func isConfined(base, target string) bool {
	if base == "" {
		return true // no confinement if cwd is unset
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
