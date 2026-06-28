package taui

import "syscall"

func ioctlGetTermios() uintptr { return syscall.TIOCGETA }
func ioctlSetTermios() uintptr { return syscall.TIOCSETA }
