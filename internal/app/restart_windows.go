//go:build windows

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// restartInPlace starts the updated binary in this console and returns, leaving
// the caller to exit. Windows has no exec(2) equivalent, so the old process
// cannot be replaced in place; a child sharing the console is the closest
// behaviour. extraArgs (e.g. a --resume pair) are placed before the original
// argv so they always parse as flags.
func restartInPlace(ctx context.Context, extraArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating tau binary: %w", err)
	}
	args := make([]string, 0, len(extraArgs)+len(os.Args)-1)
	args = append(args, extraArgs...)
	args = append(args, os.Args[1:]...)
	// Detach from the caller's shutdown context: the restarted process
	// outlives this one and must not inherit its cancellation.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restarting %s: %w", exe, err)
	}
	return nil
}
