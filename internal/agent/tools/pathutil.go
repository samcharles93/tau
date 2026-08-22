package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxSymlinkHops = 64

// resolveSymlinks resolves all symbolic links in path. If path (or part of it)
// does not exist, existing ancestors are resolved and nonexistent trailing
// components are preserved.
func resolveSymlinks(path string) (string, error) {
	return resolveSymlinksWithDepth(path, 0)
}

func resolveSymlinksWithDepth(path string, depth int) (string, error) {
	if depth > maxSymlinkHops {
		return "", errors.New("too many levels of symbolic links")
	}

	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Fast path: if the path exists, evaluate symlinks directly.
	if realPath, err := filepath.EvalSymlinks(cleanPath); err == nil {
		return realPath, nil
	}

	vol := filepath.VolumeName(cleanPath)
	rest := cleanPath[len(vol):]
	parts := strings.Split(filepath.ToSlash(rest), "/")

	current := vol
	if current == "" && strings.HasPrefix(cleanPath, "/") {
		current = "/"
	}

	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(current, part)
		fi, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				remaining := parts[i:]
				return filepath.Join(append([]string{current}, remaining...)...), nil
			}
			return "", err
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(next)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(current, linkTarget)
			}
			remaining := parts[i+1:]
			fullRemaining := linkTarget
			if len(remaining) > 0 {
				fullRemaining = filepath.Join(append([]string{linkTarget}, remaining...)...)
			}
			return resolveSymlinksWithDepth(fullRemaining, depth+1)
		}

		current = next
	}

	return current, nil
}

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

// goModCacheDir returns the Go module cache root, or "" if it cannot be
// determined. It mirrors cmd/go's own resolution order rather than shelling
// out to `go env GOMODCACHE`, so path checks stay allocation-cheap and do not
// depend on a Go toolchain being installed. It is a var so tests can point it
// at a temp dir instead of the host's real cache.
var goModCacheDir = sync.OnceValue(func() string {
	if v := os.Getenv("GOMODCACHE"); v != "" {
		return filepath.Clean(v)
	}
	if v := os.Getenv("GOPATH"); v != "" {
		// GOPATH may be a list; cmd/go uses the first entry.
		if first, _, _ := strings.Cut(v, string(os.PathListSeparator)); first != "" {
			return filepath.Join(filepath.Clean(first), "pkg", "mod")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go", "pkg", "mod")
})

// isReadConfined reports whether a read-only tool may touch target. It permits
// the workspace plus the Go module cache: sandbox_escape on module-cache paths
// was the largest avoidable error class across analysed sessions, and every
// occurrence was a legitimate attempt to read dependency source. The cache is
// deliberately NOT extended to the mutating tools (edit, write), which stay
// confined to the workspace so the agent cannot corrupt shared dependencies.
func isReadConfined(base, target string) bool {
	if isConfined(base, target) {
		return true
	}
	cache := goModCacheDir()
	return cache != "" && isConfined(cache, target)
}

// isConfined checks whether target is within (or equal to) the base directory,
// resolving any symbolic links in both base and target to prevent sandbox
// escapes via project-internal symlinks pointing outside the workspace.
// Returns false if target escapes via ../, symlinks to outside paths, or is an
// unrelated absolute path.
func isConfined(base, target string) bool {
	if base == "" {
		return true // no confinement if cwd is unset
	}
	baseReal, err := resolveSymlinks(base)
	if err != nil {
		return false
	}
	targetReal, err := resolveSymlinks(target)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(baseReal, targetReal)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
