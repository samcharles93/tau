package taui

import "os"

// sigWINCH is nil on platforms without SIGWINCH support (e.g. Windows).
// Set by platform-specific init() functions on Unix systems.
var sigWINCH os.Signal
