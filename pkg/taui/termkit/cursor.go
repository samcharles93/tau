package termkit

// Cursor and line control escape sequences. Emit() writes these only when
// colour is enabled, so they never end up in log files.
const (
	MoveUpAndClear = "\033[F\033[K" // move to start of previous line, erase it
	ClearLine      = "\r\033[K"     // carriage return, then erase to end of line
	ClearToEnd     = "\033[K"       // erase from cursor to end of line (EL)
	HideCursor     = "\033[?25l"
	ShowCursor     = "\033[?25h"
)
