package tui2

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- renderStatusBar --------------------------------------------------------

func TestRenderStatusBarBasic(t *testing.T) {
	left := []statusSeg{{text: "tau"}, {text: "gpt-4"}}
	right := []statusSeg{{text: "1.2k tok", prio: prioTokens}}

	out := renderStatusBar(80, left, right)
	if out == "" {
		t.Fatal("expected non-empty status bar")
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "tau") {
		t.Errorf("status bar should contain 'tau', got %q", plain)
	}
	if !strings.Contains(plain, "gpt-4") {
		t.Errorf("status bar should contain 'gpt-4', got %q", plain)
	}
	if !strings.Contains(plain, "1.2k tok") {
		t.Errorf("status bar should contain token count, got %q", plain)
	}
}

func TestRenderStatusBarWidthPressureDropsLowPrio(t *testing.T) {
	left := []statusSeg{{text: "LLLLLLLLLL", prio: 0}}
	right := []statusSeg{
		{text: "RRRRRR", prio: prioContext},
		{text: "costly", prio: prioCost},
		{text: "expensive", prio: prioCost},
		{text: "dropped-first", prio: 1},
	}

	out := renderStatusBar(10, left, right)
	plain := stripANSI(out)
	if strings.Contains(plain, "dropped-first") {
		t.Errorf("lowest-prio segment should be dropped under width pressure, got %q", plain)
	}
}

func TestRenderStatusBarRightOnlyFitsAll(t *testing.T) {
	right := []statusSeg{
		{text: "short", prio: 1},
		{text: "seg", prio: 2},
	}

	out := renderStatusBar(30, nil, right)
	plain := stripANSI(out)
	if !strings.Contains(plain, "short") || !strings.Contains(plain, "seg") {
		t.Errorf("expected both segments present, got %q", plain)
	}
}

func TestRenderStatusBarEmpty(t *testing.T) {
	out := renderStatusBar(80, nil, nil)
	if out == "" {
		t.Fatal("expected non-empty output even for empty segments")
	}
}

func TestRenderStatusBarMinWidth(t *testing.T) {
	out := renderStatusBar(0, []statusSeg{{text: "hello"}}, nil)
	if out == "" {
		t.Fatal("expected output even with 0 width")
	}
}

func TestRenderStatusBarLeftTruncated(t *testing.T) {
	right := []statusSeg{{text: "RRRRR", prio: 1}}
	left := []statusSeg{{text: "LLLLLLLLLLLLLLLLL", prio: 0}}

	out := renderStatusBar(10, left, right)
	plain := stripANSI(out)
	// Right segment should still be present since it has higher prio.
	if !strings.Contains(plain, "RRRRR") {
		t.Errorf("right segment should survive truncation, got %q", plain)
	}
}

// --- joinSegs ---------------------------------------------------------------

func TestJoinSegsEmpty(t *testing.T) {
	styled, plain := joinSegs(nil)
	if styled != "" || plain != "" {
		t.Fatalf("expected empty output for empty input, got styled=%q plain=%q", styled, plain)
	}
}

func TestJoinSegsSingle(t *testing.T) {
	styled, plain := joinSegs([]statusSeg{{text: "hello"}})
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
	if styled == "" {
		t.Fatal("styled should not be empty for a single segment")
	}
}

func TestJoinSegsMultiple(t *testing.T) {
	segs := []statusSeg{
		{text: "a"},
		{text: "b"},
		{text: "c"},
	}
	_, plain := joinSegs(segs)
	if plain != "a · b · c" {
		t.Fatalf("plain = %q, want %q", plain, "a · b · c")
	}
}

func TestJoinSegsSkipsEmpty(t *testing.T) {
	segs := []statusSeg{
		{text: "a"},
		{text: ""},
		{text: "b"},
	}
	_, plain := joinSegs(segs)
	if plain != "a · b" {
		t.Fatalf("plain = %q, want %q", plain, "a · b")
	}
}

func TestJoinSegsCustomStyle(t *testing.T) {
	styler := func(s string) string {
		return lipgloss.NewStyle().Bold(true).Render(s)
	}
	_, plain := joinSegs([]statusSeg{
		{text: "bold", style: styler},
	})
	if plain != "bold" {
		t.Fatalf("plain = %q, want %q", plain, "bold")
	}
}

// --- humanizeTokens ---------------------------------------------------------

func TestHumanizeTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{999999, "1000.0k"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, tc := range tests {
		got := humanizeTokens(tc.n)
		if got != tc.want {
			t.Errorf("humanizeTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// --- formatCost -------------------------------------------------------------

func TestFormatCost(t *testing.T) {
	tests := []struct {
		cost float64
		want string
	}{
		{0.0, "$0.0000"},
		{0.001234, "$0.0012"},
		{0.9999, "$0.9999"},
		{1.0, "$1.00"},
		{1.5, "$1.50"},
		{123.456, "$123.46"},
	}
	for _, tc := range tests {
		got := formatCost(tc.cost)
		if got != tc.want {
			t.Errorf("formatCost(%f) = %q, want %q", tc.cost, got, tc.want)
		}
	}
}

// --- contextPct -------------------------------------------------------------

func TestContextPct(t *testing.T) {
	tests := []struct {
		prompt    int
		ctxWindow int
		want      int
	}{
		{0, 8192, -1},
		{100, 0, -1},
		{4096, 8192, 50},
		{7373, 8192, 90},
		{8192, 8192, 100},
		{1000, 1000, 100},
	}
	for _, tc := range tests {
		got := contextPct(tc.prompt, tc.ctxWindow)
		if got != tc.want {
			t.Errorf("contextPct(%d, %d) = %d, want %d", tc.prompt, tc.ctxWindow, got, tc.want)
		}
	}
}

func TestFormatContextPct(t *testing.T) {
	if got := formatContextPct(0, 8192); got != "" {
		t.Fatalf("formatContextPct(0, 8192) = %q, want empty", got)
	}
	if got := formatContextPct(4096, 8192); got != "ctx 50%" {
		t.Fatalf("formatContextPct(4096, 8192) = %q, want %q", got, "ctx 50%")
	}
}

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

// --- truncateANSIToWidth ----------------------------------------------------

func TestTruncateANSIToWidthFits(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello")
	out := truncateANSIToWidth(s, 100, "…")
	plain := stripANSI(out)
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
}

func TestTruncateANSIToWidthTruncates(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello world")
	out := truncateANSIToWidth(s, 6, "…")
	plain := stripANSI(out)
	if visibleWidth(plain) != 6 || !strings.HasPrefix(plain, "hello") {
		t.Fatalf("truncated = %q, want 6-char prefix with ellipsis", plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
}

func TestTruncateANSIToWidthZeroBudget(t *testing.T) {
	out := truncateANSIToWidth("hello", 0, "…")
	if out != "" {
		t.Fatalf("expected empty for zero width, got %q", out)
	}
}

func TestTruncateANSIToWidthStyledOutput(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Foreground(themeHex(theme.ToneError)).Render("very long text here")
	out := truncateANSIToWidth(s, 10, "…")
	plain := stripANSI(out)
	// Truncation + ellipsis should produce <= budget.
	if visibleWidth(plain) > 10 {
		t.Fatalf("truncated plain text width = %d, want <= 10", visibleWidth(plain))
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
	// ANSI codes must not be lost by truncation.
	if !strings.HasPrefix(out, "\x1b") {
		t.Error("truncated output lost ANSI style prefix")
	}
}

// --- stripANSI --------------------------------------------------------------

func TestStripANSIPlain(t *testing.T) {
	plain := stripANSI("hello world")
	if plain != "hello world" {
		t.Fatalf("stripANSI(%q) = %q", "hello world", plain)
	}
}

func TestStripANSISingleEscape(t *testing.T) {
	plain := stripANSI("\x1b[31mhello\x1b[0m")
	if plain != "hello" {
		t.Fatalf("stripANSI with escape codes = %q, want %q", plain, "hello")
	}
}

func TestStripANSIMultipleCodes(t *testing.T) {
	plain := stripANSI("\x1b[1m\x1b[31mhello\x1b[0m world")
	if plain != "hello world" {
		t.Fatalf("stripANSI with multiple codes = %q, want %q", plain, "hello world")
	}
}

func TestStripANSINonEscapeCodes(t *testing.T) {
	plain := stripANSI("hello\x1bworld")
	if plain != "helloworld" {
		// The incomplete escape is still consumed.
		t.Logf("stripANSI handles incomplete escape, got %q", plain)
	}
}

// TestStripANSIOSC8Hyperlink guards against a regression where stripANSI
// only recognised the SGR 'm' terminator: an OSC 8 hyperlink's ST
// terminator (ESC '\') was never matched, so stripANSI kept "in escape"
// hunting for a stray 'm' anywhere in the URL or link text, silently
// eating real content (and corrupting width math for anything built from
// it, like the status bar's "web" segment).
func TestStripANSIOSC8Hyperlink(t *testing.T) {
	link := termkit.Hyperlink("web", "http://127.0.0.1:8080")
	if plain := stripANSI(link); plain != "web" {
		t.Fatalf("stripANSI(OSC8 hyperlink) = %q, want %q", plain, "web")
	}

	// A URL containing 'm' (the old bug's exact trigger) must not change
	// the outcome.
	link = termkit.Hyperlink("web", "http://example.com:8080")
	if plain := stripANSI(link); plain != "web" {
		t.Fatalf("stripANSI(OSC8 hyperlink with 'm' in URL) = %q, want %q", plain, "web")
	}
}

func TestStripANSIMalformedAndPrivateCSISequences(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"unterminated OSC": {input: "visible\x1b]8;;https://example.test", want: "visible"},
		"private CSI":      {input: "\x1b[?25lvisible", want: "visible"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := stripANSI(tt.input); got != tt.want {
				t.Fatalf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestWebSegWidthMath guards against webSeg baking its OSC 8 escape
// directly into statusSeg.text: joinSegs uses .text verbatim for width
// math, so a segment whose text is itself an escape sequence makes every
// width-pressure/truncation decision downstream wrong. webSeg must keep
// .text as the plain "web" label and carry the hyperlink via
// styledOverride instead.
func TestWebSegWidthMath(t *testing.T) {
	seg := webSeg("http://127.0.0.1:8080")
	if seg.text != "web" {
		t.Fatalf("webSeg.text = %q, want plain %q (styling belongs in styledOverride)", seg.text, "web")
	}
	if seg.styledOverride == "" {
		t.Fatal("webSeg.styledOverride should carry the OSC8 hyperlink")
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

// --- visibleWidth -----------------------------------------------------------

func TestVisibleWidth(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"hello", 5},
		{"hello world", 11},
	}
	for _, tc := range tests {
		got := visibleWidth(tc.s)
		if got != tc.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tc.s, got, tc.want)
		}
	}
}

func TestVisibleWidthStripped(t *testing.T) {
	s := lipgloss.NewStyle().Bold(true).Render("hello")
	plain := stripANSI(s)
	w := visibleWidth(plain)
	if w != 5 {
		t.Fatalf("visibleWidth of styled plain = %d, want 5", w)
	}
}

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

// TestComputeStatusBarNarrowWidthReadyKeepsModelOverLabel checks Ready's own
// degradation order: the model name (left, tail-truncated) survives longer
// than the trailing "Ready" label (right, lowest priority) under pressure.
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
