package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveToolPath resolves a potentially relative path against the working
// directory. It also strips a leading @ because some models include it when
// referring to files.
func resolveToolPath(cwd, path string) string {
	path = strings.TrimSpace(strings.TrimPrefix(path, "@"))
	if path == "" {
		if cwd == "" {
			return "."
		}
		return filepath.Clean(cwd)
	}
	if filepath.IsAbs(path) || cwd == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

// pathWithinRoot reports whether target is contained within root.
// Both paths are normalised to absolute paths before comparison.
func pathWithinRoot(root, target string) (bool, error) {
	if strings.TrimSpace(root) == "" {
		return true, nil
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("resolve root path: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, fmt.Errorf("resolve target path: %w", err)
	}

	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false, fmt.Errorf("compare path to root: %w", err)
	}
	if rel == "." {
		return true, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// resolvePathWithinRoot resolves path against cwd and rejects paths that escape
// the configured working root.
func resolvePathWithinRoot(cwd, path string) (string, error) {
	resolved := resolveToolPath(cwd, path)
	ok, err := pathWithinRoot(cwd, resolved)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path escapes working directory: %s", resolved)
	}
	return resolved, nil
}

// validateWorkingDir checks that cwd exists and is a directory.
func validateWorkingDir(cwd string) error {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("invalid cwd %q: %w", cwd, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("invalid cwd %q: not a directory", cwd)
	}
	return nil
}

// writeFileAtomic replaces path atomically using a temp file in the same directory.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
