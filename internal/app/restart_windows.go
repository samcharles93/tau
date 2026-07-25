//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
)

// restartInPlace starts the updated binary in this console and returns, leaving
// the caller to exit. Windows has no exec(2) equivalent, so the old process
// cannot be replaced in place; a child sharing the console is the closest
// behaviour.
func restartInPlace() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating tau binary: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restarting %s: %w", exe, err)
	}
	return nil
}
