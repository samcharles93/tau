package tui2

import (
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
)

// The core layout/width-pressure algorithm (renderStatusBar/joinSegs), the
// shared formatters (humanizeTokens/formatCost/contextPct/formatContextPct),
// and the width primitives (visibleWidth/truncateANSIToWidth/stripANSI) now
// live in pkg/taui (statusline.go, utils.go) and are covered exhaustively
// there (narrow widths, empty groups, Unicode, ANSI/OSC hyperlinks,
// undroppable segments, formatting boundaries - see
// pkg/taui/statusline_test.go and pkg/taui/utils_test.go). What remains
// here is this frontend's own styling glue (lipgloss/theme-based
// contextStyle, statusGrey) and the agent-state-driven segment assembly
// (computeStatusBar and its helpers), which internal/tui doesn't share
// since it has a different state model entirely.

// --- contextStyle -----------------------------------------------------------

func TestContextStyle(t *testing.T) {
	tests := []struct {
		pct  int
		name string
	}{
		{50, "green (low)"},
		{80, "nil (medium)"},
		{90, "amber (high)"},
		{95, "red (critical)"},
	}
	for _, tc := range tests {
		style := contextStyle(tc.pct)
		if tc.pct >= 90 && style == nil {
			t.Errorf("contextStyle(%d) = nil, wanted a style for %s", tc.pct, tc.name)
		}
		if tc.pct < 75 && style != nil {
			t.Errorf("contextStyle(%d) = non-nil, wanted nil for %s", tc.pct, tc.name)
		}
	}
}

// TestWebSegWidthMath guards against webSeg baking its OSC 8 escape
// directly into StatusLineSeg.Text: joinSegs uses .Text verbatim for width
// math, so a segment whose text is itself an escape sequence makes every
// width-pressure/truncation decision downstream wrong. webSeg must keep
// .Text as the plain "web" label and carry the hyperlink via
// StyledOverride instead.
func TestWebSegWidthMath(t *testing.T) {
	seg := webSeg("http://127.0.0.1:8080")
	if seg.Text != "web" {
		t.Fatalf("webSeg.Text = %q, want plain %q (styling belongs in StyledOverride)", seg.Text, "web")
	}
	if seg.StyledOverride == "" {
		t.Fatal("webSeg.StyledOverride should carry the OSC8 hyperlink")
	}
	_, plain := joinSegs([]statusSeg{seg})
	if plain != "web" {
		t.Fatalf("joinSegs plain output = %q, want %q", plain, "web")
	}
	if w := visibleWidth(plain); w != 3 {
		t.Fatalf("visibleWidth(%q) = %d, want 3", plain, w)
	}
}

// --- boldText ---------------------------------------------------------------

func TestBoldTextWrapsWithANSI(t *testing.T) {
	out := boldText("important")
	if !strings.HasPrefix(out, "\x1b") {
		t.Error("boldText should apply ANSI bold style")
	}
	plain := stripANSI(out)
	if plain != "important" {
		t.Fatalf("boldText plain = %q, want %q", plain, "important")
	}
}

// --- computeStatusBar --------------------------------------------------------

func TestComputeStatusBarBasic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.provider = "openai"
	m.width = 80

	bar := m.computeStatusBar()
	if bar == "" {
		t.Fatal("expected non-empty status bar")
	}
	plain := stripANSI(bar)
	if !strings.Contains(plain, "gpt-4") {
		t.Fatalf("status bar = %q, want 'gpt-4'", plain)
	}
}

func TestComputeStatusBarProcessingState(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.provider = "openai"
	m.width = 80
	m.agentState = agentProcessing

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Processing") {
		t.Fatalf("status bar = %q, want 'Processing'", plain)
	}
	if !strings.Contains(plain, "Ctrl+C Stop") {
		t.Fatalf("status bar = %q, want interrupt hint", plain)
	}
	if !strings.Contains(plain, "gpt-4") {
		t.Fatalf("status bar = %q, want model name on right side", plain)
	}
	if strings.Contains(plain, "openai") {
		t.Fatalf("status bar = %q, Processing should NOT show provider", plain)
	}
	if strings.Contains(plain, "Ready") || strings.Contains(plain, "Thinking") {
		t.Fatalf("status bar = %q, should only show Processing, not Ready or Thinking", plain)
	}
}

func TestComputeStatusBarSteering(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.provider = "openai"
	m.steering = true
	m.width = 80

	bar := m.computeStatusBar()
	plain := stripANSI(bar)
	if !strings.Contains(plain, "steering") {
		t.Fatalf("status bar = %q, want 'steering'", plain)
	}
}

func TestComputeStatusBarEffort(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.reasoningEffort = "high"
	m.width = 80

	bar := m.computeStatusBar()
	plain := stripANSI(bar)
	if !strings.Contains(plain, "high") {
		t.Fatalf("status bar = %q, want 'high' effort", plain)
	}
}

func TestComputeStatusBarLabelsSessionTokenTotals(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))

	pub.Publish(tauchat.MetricEvent{
		Category: tauchat.MetricCategoryLLM,
		Name:     "llm.response",
		Value:    20_000,
		Labels: map[string]string{
			"prompt_tokens":     "19000",
			"completion_tokens": "1000",
		},
		SessionID: "sess",
	})
	for range 100 {
		if totals := tracker.Snapshot("sess"); totals != nil && totals.TotalTokens == 20_000 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = tracker
	m.ctxWindow = 200_000
	m.width = 120

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "↑19.0k ↓1.0k") {
		t.Fatalf("status bar = %q, want the input/output token split", plain)
	}
}

// TestComputeStatusBarNarrowWidthPrioritizesActiveState checks the core
// width-pressure requirement: under a narrow terminal, the active-state
// label and interrupt hint must survive while secondary metadata (session
// tokens, cost, context %) gets dropped.
func TestComputeStatusBarNarrowWidthPrioritizesActiveState(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))
	pub.Publish(tauchat.MetricEvent{
		Category:  tauchat.MetricCategoryLLM,
		Name:      "llm.response",
		Value:     20_000,
		Labels:    map[string]string{"prompt_tokens": "19000", "completion_tokens": "1000"},
		SessionID: "sess",
	})
	for range 100 {
		if totals := tracker.Snapshot("sess"); totals != nil && totals.TotalTokens == 20_000 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = tracker
	m.ctxWindow = 200_000
	m.handleChatEvent(tauchat.ChatResponseStartedEvent{})
	m.handleChatEvent(tauchat.ChatResponseDeltaEvent{Delta: "hello"})

	m.width = 18 // narrow enough to force dropping
	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ctrl+C") {
		t.Fatalf("narrow status bar = %q, want the interrupt hint to survive", plain)
	}
	if strings.Contains(plain, "session tok") {
		t.Fatalf("narrow status bar = %q, session token metadata should have been dropped first", plain)
	}
}

// TestComputeStatusBarNarrowWidthReadyPrioritizesStateLabel checks Ready's
// own degradation order under extreme width pressure: the right-hand group
// (here, just the "Ready" state label) is never truncated away - only the
// left identity blob (τ tau/model/provider) yields, via tail-truncation -
// matching renderStatusBar's existing invariant and this feature's
// "prioritize active state over secondary metadata" requirement, treating
// "Ready" itself as Ready's active-state label.
func TestComputeStatusBarNarrowWidthReadyPrioritizesStateLabel(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	// Wide enough for "τ · Ready" (the undroppable left floor) plus a
	// little room, but too narrow for "gpt-4" to also fit on the right.
	m.width = 14

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "Ready") {
		t.Fatalf("narrow status bar = %q, want the Ready state label to survive", plain)
	}
	if strings.Contains(plain, "gpt-4") {
		t.Fatalf("narrow status bar = %q, want the model name dropped before the state label truncates", plain)
	}
}

// TestComputeStatusBarExtremeNarrowWidthDoesNotPanic checks the true
// rock-bottom case: even "τ tau · Ready" alone doesn't fit. There's nothing
// left to drop at that point (both are prioTransient), so the
// character-truncation fallback is the documented last resort - this just
// guards it stays non-empty and never panics, not any particular content.
func TestComputeStatusBarExtremeNarrowWidthDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.width = 5

	if out := m.computeStatusBar(); out == "" {
		t.Fatal("expected non-empty output even at extreme narrow width")
	}
}

func TestComputeStatusBarNarrowThinkingStateDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 10
	m.modelName = "gpt-4"
	m.agentState = agentThinking

	plain := stripANSI(m.computeStatusBar())
	if plain == "" {
		t.Fatal("expected a non-empty narrow Thinking status bar")
	}
	if visibleWidth(plain) > m.width {
		t.Fatalf("status bar width = %d, want <= %d: %q", visibleWidth(plain), m.width, plain)
	}
}

func TestComputeStatusBarZeroWidthDoesNotPanic(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.modelName = "gpt-4"
	m.width = 0

	// Should not panic; content is unconstrained by an actual terminal width
	// until the first WindowSizeMsg, same as the rest of the layout.
	_ = m.computeStatusBar()
}

func TestApproxTokensPerSecondUnavailableTooEarly(t *testing.T) {
	if _, ok := approxTokensPerSecond("some streamed text", 100*time.Millisecond); ok {
		t.Fatal("expected tok/s to be unavailable before enough elapsed time has passed")
	}
}

func TestApproxTokensPerSecondUnavailableWithNoText(t *testing.T) {
	if _, ok := approxTokensPerSecond("", 2*time.Second); ok {
		t.Fatal("expected tok/s to be unavailable with no streamed text yet")
	}
}

func TestApproxTokensPerSecondAvailable(t *testing.T) {
	rate, ok := approxTokensPerSecond(strings.Repeat("word ", 100), 2*time.Second)
	if !ok {
		t.Fatal("expected tok/s to be available with enough text and elapsed time")
	}
	if rate <= 0 {
		t.Fatalf("rate = %d, want > 0", rate)
	}
}

func TestApproxTokensPerSecondCountsRunes(t *testing.T) {
	rate, ok := approxTokensPerSecond("你好世界你好世界", time.Second)
	if !ok {
		t.Fatal("expected tok/s to be available for non-ASCII text")
	}
	if rate != 2 { // 8 runes / 4 estimated chars per token / 1 second.
		t.Fatalf("rate = %d, want 2", rate)
	}
}

func TestComputeStatusBarContextUsesLatestPromptTokens(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	tracker := metrics.NewUsageTracker(bus.Client("usage"))
	t.Cleanup(tracker.Close)
	pub := eventbus.Publish[tauchat.MetricEvent](bus.Client("coordinator"))

	for _, prompt := range []string{"500", "100"} {
		pub.Publish(tauchat.MetricEvent{
			Category: tauchat.MetricCategoryLLM,
			Name:     "llm.response",
			Value:    1000,
			Labels: map[string]string{
				"prompt_tokens":     prompt,
				"completion_tokens": "500",
			},
			SessionID: "sess",
		})
	}
	for range 100 {
		if totals := tracker.Snapshot("sess"); totals != nil && totals.TurnCount == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.usage = tracker
	m.ctxWindow = 1000
	m.width = 120

	plain := stripANSI(m.computeStatusBar())
	if !strings.Contains(plain, "ctx 10%") {
		t.Fatalf("status bar = %q, want latest prompt context percentage", plain)
	}
	if strings.Contains(plain, "ctx 60%") {
		t.Fatalf("status bar = %q, used cumulative prompt tokens for context", plain)
	}
}
