package taui

import (
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Loader is an animated spinner component. Ported from Pi's components/loader.ts.
type Loader struct {
	Text
	baseMessage  string              // the message without spinner prefix
	spinnerFn    func(string) string // colour for the spinner frame
	msgFn        func(string) string // colour for the message
	frames       []string
	interval     time.Duration
	currentFrame int
	running      bool
	stopCh       chan struct{}
	onTick       func() // called after each frame to trigger re-render
}

// Default SpinnerFrames from termkit.
var DefaultFrames = termkit.SpinnerFrames

// NewLoader creates a spinner with the given message and colour callbacks.
func NewLoader(message string, spinnerFn, msgFn func(string) string) *Loader {
	l := &Loader{
		Text:        *NewText(message),
		baseMessage: message,
		spinnerFn:   spinnerFn,
		msgFn:       msgFn,
		frames:      append([]string(nil), DefaultFrames...),
		interval:    80 * time.Millisecond,
	}
	l.updateDisplay() // show initial frame + coloured message before Start()
	return l
}

// SetIndicator configures the spinner animation.
func (l *Loader) SetIndicator(frames []string, intervalMs int) {
	if frames != nil {
		l.frames = append([]string(nil), frames...)
	}
	if intervalMs > 0 {
		l.interval = time.Duration(intervalMs) * time.Millisecond
	}
	l.currentFrame = 0
}

// OnTick registers a callback invoked after each frame change.
func (l *Loader) OnTick(fn func()) {
	l.onTick = fn
}

// Start begins the animation.
func (l *Loader) Start() {
	if l.running {
		return
	}
	l.running = true
	l.stopCh = make(chan struct{})
	l.updateDisplay()
	go l.animate()
}

// Stop halts the animation.
func (l *Loader) Stop() {
	if !l.running {
		return
	}
	l.running = false
	close(l.stopCh)
}

// SetMessage updates the displayed message.
func (l *Loader) SetMessage(msg string) {
	l.baseMessage = msg
	l.updateDisplay()
}

// Render returns the spinner line.
func (l *Loader) Render(width int) []string {
	// Blank line above for spacing, then the spinner line.
	return []string{"", l.Text.Render(width)[0]}
}

func (l *Loader) animate() {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.currentFrame = (l.currentFrame + 1) % len(l.frames)
			l.updateDisplay()
			if l.onTick != nil {
				l.onTick()
			}
		}
	}
}

func (l *Loader) updateDisplay() {
	frame := l.frames[l.currentFrame]
	indicator := frame
	if l.spinnerFn != nil && termkit.ColorEnabled() {
		indicator = l.spinnerFn(frame)
	}
	indicator += " "
	msg := l.baseMessage
	if l.msgFn != nil && termkit.ColorEnabled() {
		msg = l.msgFn(l.baseMessage)
	}
	l.SetText(indicator + msg)
}
