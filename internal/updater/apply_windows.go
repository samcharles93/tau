//go:build windows

package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

// commitBinary replaces targetPath with the staged newPath. A running
// executable cannot be removed or overwritten on Windows, but it can be
// renamed — so the swap goes through a two-step rename with rollback:
//
//  1. rename target -> .old   (move the running exe aside)
//  2. rename .new   -> target (install the new binary)
//
// If step 2 fails the old binary is restored; if that rollback also fails the
// error reports both failures so the caller can tell the user to recover
// manually.
func commitBinary(newPath, targetPath string) error {
	oldPath := filepath.Join(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".old")

	// Windows renames fail if the destination already exists — clear any
	// stale backup from a previous update first.
	_ = os.Remove(oldPath)

	if err := os.Rename(targetPath, oldPath); err != nil {
		return fmt.Errorf("moving existing binary aside: %w", err)
	}
	if err := os.Rename(newPath, targetPath); err != nil {
		if rerr := os.Rename(oldPath, targetPath); rerr != nil {
			return fmt.Errorf("installing new binary: %w; rollback failed: %v", err, rerr)
		}
		return fmt.Errorf("installing new binary: %w", err)
	}
	// Success. The old binary is still held open by the running process, so
	// it cannot be removed; hide it instead.
	if err := os.Remove(oldPath); err != nil {
		_ = hideFile(oldPath)
	}
	return nil
}
