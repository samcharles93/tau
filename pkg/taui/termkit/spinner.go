package termkit

// SpinnerFrames is a smooth braille spinner. Each frame is a single cell wide,
// so overwriting with \r keeps the line stable.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrame returns the frame at the given tick index, wrapping around.
func SpinnerFrame(tick int) string {
	return SpinnerFrames[tick%len(SpinnerFrames)]
}
