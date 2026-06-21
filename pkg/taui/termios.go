package taui

import (
	"syscall"
	"unsafe"
)

// TermiosState holds the saved terminal settings.
type TermiosState struct {
	Termios syscall.Termios
}

// Winsize holds terminal dimensions.
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// MakeRaw puts the terminal into raw mode.
func MakeRaw(fd uintptr) (*TermiosState, error) {
	var old syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old)), 0, 0, 0); err != 0 {
		return nil, err
	}
	state := &TermiosState{Termios: old}

	raw := old
	// Input flags: disable BRKINT, ICRNL, INPCK, ISTRIP, IXON
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	// Output flags: disable OPOST
	raw.Oflag &^= syscall.OPOST
	// Control flags: set CS8
	raw.Cflag |= syscall.CS8
	// Local flags: disable ECHO, ICANON, IEXTEN, ISIG
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	// Control characters: minimum 1 byte, no timeout
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&raw)), 0, 0, 0); err != 0 {
		return state, err
	}
	return state, nil
}

// Restore restores saved terminal settings.
func Restore(fd uintptr, state *TermiosState) error {
	if state == nil {
		return nil
	}
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&state.Termios)), 0, 0, 0)
	if err != 0 {
		return err
	}
	return nil
}

// GetWinsize returns the terminal window size.
func GetWinsize(fd uintptr) (*Winsize, error) {
	ws := &Winsize{}
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(ws)), 0, 0, 0); err != 0 {
		return nil, err
	}
	return ws, nil
}
