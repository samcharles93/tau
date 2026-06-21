package termkit

import "strings"

// ProgressBar renders an N-wide bar using partial block characters for sub-cell
// resolution (▏▎▍▌▋▊▉█), so motion looks smooth even at low widths.
func ProgressBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	blocks := []rune("▏▎▍▌▋▊▉█")
	full := fraction * float64(width)
	whole := int(full)
	var b strings.Builder
	for i := 0; i < whole && i < width; i++ {
		b.WriteRune('█')
	}
	if whole < width {
		frac := full - float64(whole)
		idx := int(frac * float64(len(blocks)))
		if idx > 0 {
			b.WriteRune(blocks[idx-1])
			whole++
		}
	}
	for i := whole; i < width; i++ {
		b.WriteRune(' ')
	}
	return "[" + b.String() + "]"
}
