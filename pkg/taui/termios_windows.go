package taui

import (
	"golang.org/x/sys/windows"
)

// Windows console mode flags (not all are exported by x/sys/windows).
const (
	enableEchoInput          = 0x0004
	enableLineInput          = 0x0002
	enableProcessedInput     = 0x0001
	enableWindowInput        = 0x0008
	enableMouseInput         = 0x0010
	enableInsertMode         = 0x0020
	enableQuickEditMode      = 0x0040
	enableExtendedFlags      = 0x0080
	enableVirtTermInput      = 0x0200
	enableVirtTermProcessing = 0x0004
)

// TermiosState holds the saved console mode on Windows.
type TermiosState struct {
	Mode      uint32
	OutMode   uint32
	Handle    windows.Handle
	OutHandle windows.Handle
}

// MakeRaw puts the Windows console into raw mode.
func MakeRaw(fd uintptr) (*TermiosState, error) {
	inHandle := windows.Handle(fd)
	outHandle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		return nil, err
	}

	var inMode uint32
	if err := windows.GetConsoleMode(inHandle, &inMode); err != nil {
		return nil, err
	}

	var outMode uint32
	if err := windows.GetConsoleMode(outHandle, &outMode); err != nil {
		outMode = 0
	}

	state := &TermiosState{
		Mode:      inMode,
		OutMode:   outMode,
		Handle:    inHandle,
		OutHandle: outHandle,
	}

	// Disable echo, line input, processed input, window/mouse input, quick edit.
	rawIn := inMode & ^uint32(
		enableEchoInput|enableLineInput|enableProcessedInput|
			enableWindowInput|enableMouseInput|enableQuickEditMode|
			enableInsertMode,
	)
	// Enable virtual terminal input for escape sequence processing.
	rawIn |= enableVirtTermInput | enableExtendedFlags

	if err := windows.SetConsoleMode(inHandle, rawIn); err != nil {
		return state, err
	}

	// Enable virtual terminal processing on output for ANSI escape codes.
	rawOut := outMode | enableVirtTermProcessing
	if err := windows.SetConsoleMode(outHandle, rawOut); err != nil {
		// Non-fatal: some terminals don't support VT processing.
		_ = err
	}

	return state, nil
}

// Restore restores saved console mode.
func Restore(_ uintptr, state *TermiosState) error {
	if state == nil {
		return nil
	}
	if err := windows.SetConsoleMode(state.Handle, state.Mode); err != nil {
		return err
	}
	if state.OutMode != 0 {
		return windows.SetConsoleMode(state.OutHandle, state.OutMode)
	}
	return nil
}

// GetWinsize returns the console window size on Windows.
func GetWinsize(fd uintptr) (*Winsize, error) {
	handle := windows.Handle(fd)
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(handle, &info); err != nil {
		return nil, err
	}
	return &Winsize{
		Row: uint16(info.Window.Bottom - info.Window.Top + 1),
		Col: uint16(info.Window.Right - info.Window.Left + 1),
	}, nil
}
