package updater

import (
	"fmt"
	"strings"
)

// Target is a supported release build target: a GOOS/GOARCH pair that
// .goreleaser.yaml actually publishes an archive for.
//
// This is the single source of truth for the platform matrix. install.sh
// and install.ps1 can't import it directly (they run before Go is on the
// machine), so they mirror it inline - keep those in sync by hand whenever
// this list changes.
type Target struct {
	OS   string
	Arch string
}

// SupportedTargets returns every GOOS/GOARCH pair published in a release.
// Must match .goreleaser.yaml's builds.goos/goarch matrix exactly.
func SupportedTargets() []Target {
	return []Target{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
	}
}

func isSupportedTarget(goos, goarch string) bool {
	for _, t := range SupportedTargets() {
		if t.OS == goos && t.Arch == goarch {
			return true
		}
	}
	return false
}

// ArchiveName returns the canonical release archive filename for the given
// tag and target, e.g. "tau_1.2.3_linux_amd64.tar.gz". It is the single
// source of truth for archive naming: goreleaser's archives.name_template
// and install.sh/install.ps1's archive construction must all agree with it.
func ArchiveName(tag, goos, goarch string) (string, error) {
	if tag == "" {
		return "", fmt.Errorf("release tag is empty")
	}
	if !isSupportedTarget(goos, goarch) {
		return "", fmt.Errorf("unsupported release target %s/%s", goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("tau_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext), nil
}
