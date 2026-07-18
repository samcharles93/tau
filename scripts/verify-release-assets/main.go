// Command verify-release-assets checks a goreleaser dist/ directory against
// internal/updater.SupportedTargets(), the single source of truth for the
// release platform matrix. It fails if any supported target's archive is
// missing from dist/, or if dist/checksums.txt has no entry for it.
//
// Intended to run in CI right after a goreleaser dry-run/build step and
// before the actual publish step, so a misconfigured goreleaser (a target
// dropped from builds.goos/goarch, a broken archive name template, etc.)
// fails CI instead of silently shipping an incomplete release.
//
// Usage:
//
//	go run ./scripts/verify-release-assets --dist dist --tag v1.2.3
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/internal/updater"
)

func main() {
	distDir := flag.String("dist", "dist", "goreleaser output directory to verify")
	tag := flag.String("tag", "", "release tag/version, e.g. v1.2.3 (required)")
	flag.Parse()

	if err := run(*distDir, *tag); err != nil {
		fmt.Fprintln(os.Stderr, "verify-release-assets:", err)
		os.Exit(1)
	}
	fmt.Println("verify-release-assets: all supported platform archives are present with checksums")
}

func run(distDir, tag string) error {
	if tag == "" {
		return fmt.Errorf("--tag is required")
	}

	checksums, err := parseChecksums(filepath.Join(distDir, "checksums.txt"))
	if err != nil {
		return err
	}

	var problems []string
	for _, target := range updater.SupportedTargets() {
		name, err := updater.ArchiveName(tag, target.OS, target.Arch)
		if err != nil {
			return fmt.Errorf("target %s/%s: %w", target.OS, target.Arch, err)
		}

		if _, err := os.Stat(filepath.Join(distDir, name)); err != nil {
			problems = append(problems, fmt.Sprintf("%s: archive not found in %s", name, distDir))
			continue
		}

		if _, ok := checksums[name]; !ok {
			problems = append(problems, fmt.Sprintf("%s: no checksums.txt entry", name))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("release asset matrix incomplete:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// parseChecksums reads a goreleaser checksums.txt ("<sha256>  <filename>" per
// line) into a filename -> checksum map.
func parseChecksums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	defer f.Close()

	entries := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		entries[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	return entries, nil
}
