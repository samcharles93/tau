//go:build !windows

package plugin

import "os"

// isExecutableByPlatform checks Unix execute permission bits.
func isExecutableByPlatform(_ string, info os.FileInfo) bool {
	return info.Mode()&0o111 != 0
}
