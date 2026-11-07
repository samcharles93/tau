//go:build !windows

package app

import (
	"fmt"
	"os"
	"syscall"
)

// restartInPlace replaces this process with a fresh exec of the (just updated)
// binary, reusing the same environment and terminal. extraArgs (e.g. a
// --resume pair) are placed before the original argv so they always parse as
// flags, even when the original invocation had positional args. It only
// returns on failure: on success the kernel discards this image, so it must
// be called after the TUI has exited and every resource is released.
func restartInPlace(extraArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating tau binary: %w", err)
	}
	argv := append([]string{exe}, extraArgs...)
	argv = append(argv, os.Args[1:]...)
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return fmt.Errorf("re-exec %s: %w", exe, err)
	}
	return nil
}
