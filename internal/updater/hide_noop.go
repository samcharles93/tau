//go:build !windows

package updater

// hideFile is a no-op outside Windows, where the old binary can simply be
// removed after a successful swap.
func hideFile(string) error { return nil }
