//go:build !windows

package app

import (
	"fmt"
	"os"
	"syscall"
)

// restartInPlace replaces this process with a fresh exec of the (just updated)
// binary, reusing the same argv, environment, and terminal. It only returns on
// failure: on success the kernel discards this image, so it must be called
// after the TUI has exited and every resource is released.
func restartInPlace() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating tau binary: %w", err)
	}
	argv := append([]string{exe}, os.Args[1:]...)
	if err := syscall.Exec(exe, argv, os.Environ()); err != nil {
		return fmt.Errorf("re-exec %s: %w", exe, err)
	}
	return nil
}
