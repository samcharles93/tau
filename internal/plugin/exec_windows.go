//go:build windows

package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// windowsExecutableExts lists extensions that Windows treats as executable.
// Compiled Go plugins are the expected use case; .ps1 is omitted because
// PowerShell scripts require an interpreter and are not self-executable.
var windowsExecutableExts = []string{".exe", ".bat", ".cmd", ".com"}

// isExecutableByPlatform checks whether the file has a Windows-executable extension.
func isExecutableByPlatform(path string, _ os.FileInfo) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range windowsExecutableExts {
		if ext == e {
			return true
		}
	}
	return false
}
