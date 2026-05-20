package completions

// CommandDef defines a slash command available in the TUI.
type CommandDef struct {
	// Name is the full command string including the leading slash (e.g. "/new").
	Name string
	// Description is a short human-readable help string.
	Description string
	// AcceptsArgs indicates whether the command takes arguments after the name.
	AcceptsArgs bool
}
