package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

// applyUpdate atomically replaces the binary at targetPath with newBinary.
//
// Integrity is verified before this point: the release archive is checked
// against checksums.txt (SHA-256) and the extracted binary must pass a
// --version smoke test. This function only performs the file swap.
//
// This replaces the previous use of github.com/minio/selfupdate, whose
// golang.org/x/crypto/openpgp dependency carries the unfixable advisory
// GO-2026-5932 (unmaintained, unsafe by design). tau's trust model is
// SHA-256 checksums over TLS, not PGP signatures, so the only primitive the
// updater needs is an atomic executable replacement — implemented here
// in-house instead of pulling in a signature library.
func applyUpdate(newBinary []byte, targetPath string) error {
	dir := filepath.Dir(targetPath)
	name := filepath.Base(targetPath)
	// Stage the new binary next to the target so the final rename stays on
	// the same filesystem and can be atomic.
	newPath := filepath.Join(dir, "."+name+".new")
	if err := writeStagedBinary(newPath, newBinary); err != nil {
		return err
	}
	defer os.Remove(newPath) // no-op after a successful commit; cleans up on failure

	return commitBinary(newPath, targetPath)
}

// writeStagedBinary writes the new binary with an executable mode and closes
// it before returning (Windows refuses to move a file that is still open).
func writeStagedBinary(path string, binary []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("creating staging file %s: %w", path, err)
	}
	if _, err := f.Write(binary); err != nil {
		f.Close()
		return fmt.Errorf("writing staging file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing staging file %s: %w", path, err)
	}
	return nil
}
