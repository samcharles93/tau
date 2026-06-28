//go:build unix

package taui

import "syscall"

func init() { sigWINCH = syscall.SIGWINCH }
