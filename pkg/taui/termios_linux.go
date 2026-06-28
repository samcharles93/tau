package taui

import "syscall"

func ioctlGetTermios() uintptr { return syscall.TCGETS }
func ioctlSetTermios() uintptr { return syscall.TCSETS }
