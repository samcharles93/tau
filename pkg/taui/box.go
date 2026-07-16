package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// BgFn is a callback that applies a background colour to a string.
type BgFn func(text string) string

// FgFn is a callback that applies a foreground colour to a string.
type FgFn func(text string) string

// Box is a container that applies padding and background colour to its
// children. Build it with the BoxBuilder:
//
//	box := NewBox().
//	    Padding(2, 1).
//	    Bg(amberBg).
//	    ExpandW().    // fill terminal width
//	    ExpandH().    // fill terminal height
//	    Build()
type Box struct {
	Container
	padX    int
	padY    int
	bgFn    BgFn
	expandW bool // pad to full terminal width
	expandH bool // fill remaining terminal height

	cache *boxCache
}

type boxCache struct {
	width      int
	childLines []string
	bgHash     string
	lines      []string
}

// BoxBuilder constructs a Box.
type BoxBuilder struct {
	box Box
}

// NewBox starts a Box builder.
func NewBox() *BoxBuilder {
	return &BoxBuilder{box: Box{padX: 1, padY: 1}}
}

// Padding sets the horizontal and vertical padding.
func (b *BoxBuilder) Padding(x, y int) *BoxBuilder {
	b.box.padX = x
	b.box.padY = y
	return b
}

// Bg sets the background colour callback.
func (b *BoxBuilder) Bg(fn BgFn) *BoxBuilder {
	b.box.bgFn = fn
	return b
}

// ExpandW makes the box pad every line to the full terminal width.
func (b *BoxBuilder) ExpandW() *BoxBuilder {
	b.box.expandW = true
	return b
}

// ExpandH makes the box fill remaining terminal height with empty
// background-coloured lines.
func (b *BoxBuilder) ExpandH() *BoxBuilder {
	b.box.expandH = true
	return b
}

// Build returns the configured Box.
func (b *BoxBuilder) Build() *Box {
	return &b.box
}

// SetBgFn updates the background callback at runtime.
func (b *Box) SetBgFn(fn BgFn) { b.bgFn = fn }

// Fill pads lines to fill the terminal height with bg-coloured empty lines
// when ExpandH() is set. No-op otherwise.
func (b *Box) Fill(lines []string, width, height int) []string {
	if !b.expandH || height <= len(lines) {
		return lines
	}
	for len(lines) < height {
		lines = append(lines, b.applyBg("", width))
	}
	return lines
}

// Invalidate clears the render cache.
func (b *Box) Invalidate() {
	b.cache = nil
	b.Container.Invalidate()
}

// Render renders the box at the given width with padding and background.
func (b *Box) Render(width int) []string {
	if len(b.Children) == 0 {
		return nil
	}

	contentWidth := max(1, width-b.padX*2)
	leftPad := strings.Repeat(" ", b.padX)

	var childLines []string
	for _, child := range b.Children {
		for _, line := range child.Render(contentWidth) {
			childLines = append(childLines, leftPad+line)
		}
	}
	if len(childLines) == 0 {
		return nil
	}

	// Sample bgFn for cache key.
	bgHash := ""
	if b.bgFn != nil && termkit.ColorEnabled() {
		bgHash = b.bgFn("test")
	}

	if b.cache != nil &&
		b.cache.width == width &&
		b.cache.bgHash == bgHash &&
		linesEqual(b.cache.childLines, childLines) {
		return b.cache.lines
	}

	var result []string

	// Top padding.
	for range b.padY {
		result = append(result, b.applyBg("", width))
	}

	// Content.
	for _, line := range childLines {
		result = append(result, b.applyBg(line, width))
	}

	// Bottom padding.
	for range b.padY {
		result = append(result, b.applyBg("", width))
	}

	b.cache = &boxCache{
		width:      width,
		childLines: childLines,
		bgHash:     bgHash,
		lines:      result,
	}
	return result
}

func (b *Box) applyBg(line string, width int) string {
	if !b.expandW {
		// Natural width - no padding.
		if b.bgFn != nil && termkit.ColorEnabled() {
			return b.bgFn(line)
		}
		return line
	}
	visLen := VisibleWidth(line)
	padNeeded := max(0, width-visLen)
	padded := line + strings.Repeat(" ", padNeeded)
	if b.bgFn != nil && termkit.ColorEnabled() {
		return b.bgFn(padded)
	}
	return padded
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
