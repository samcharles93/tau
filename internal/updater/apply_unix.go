//go:build !windows

package updater

import (
	"fmt"
	"os"
)

// commitBinary replaces targetPath with the staged newPath. On Unix,
// os.Rename over an existing file is atomic: any reader observes either the
// old or the new binary, never a missing file, so no backup/rollback dance is
// needed.
func commitBinary(newPath, targetPath string) error {
	if err := os.Rename(newPath, targetPath); err != nil {
		return fmt.Errorf("replacing %s: %w", targetPath, err)
	}
	return nil
}
