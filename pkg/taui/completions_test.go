package taui

import (
	"testing"
	"time"
)

func TestCompletionsHiddenByDefault(t *testing.T) {
	input := NewLineInput("")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return nil
	})
	if c.Visible() {
		t.Fatal("completions should be hidden when provider returns nil")
	}
}

func TestCompletionsShowsWhenProviderReturns(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "fx" {
		input.HandleInput(string(r))
	}
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		if ctx.Text == "" {
			return nil
		}
		return &CompletionSet{
			ReplaceStart: 0,
			ReplaceEnd:   len([]rune(ctx.Text)),
			Groups: []MatchGroup{{
				Title: "Actions",
				Matches: []Match{
					{Word: "fix", Description: "— fix the build"},
					{Word: "build"},
				},
			}},
		}
	})
	c.Render(60)
	if !c.Visible() {
		t.Fatal("completions should be visible when provider returns items")
	}
	lines := c.Render(60)
	if len(lines) == 0 {
		t.Fatal("rendered nothing for visible completions")
	}
}

func TestCompletionsFuzzyFilters(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "fx" {
		input.HandleInput(string(r))
	}
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0,
			ReplaceEnd:   len([]rune(ctx.Text)),
			Groups: []MatchGroup{{
				Matches: []Match{
					{Word: "fix", Description: "— fix the build"},
					{Word: "fox"},
					{Word: "build"},
				},
			}},
		}
	})
	// "fx" fuzzy-matches "fix" and "fox" but NOT "build" (no 'f').
	c.Render(60) // triggers refresh + filter
	// Verify filtered state: should only contain fix + fox.
	c.mu.Lock()
	vis := c.visible
	count := len(c.filtered)
	c.mu.Unlock()
	if !vis {
		t.Fatal("should be visible with matching items")
	}
	if count != 2 {
		t.Fatalf("filtered count = %d, want 2 (only fix + fox match 'fx')", count)
	}
}

func TestCompletionsNavigateAndSelect(t *testing.T) {
	input := NewLineInput("")
	// "a" matches "alpha" and "gamma" but NOT "beta" — fuzzy filter drops beta.
	for _, r := range "a" {
		input.HandleInput(string(r))
	}
	var got string
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 1,
			Groups: []MatchGroup{{
				Matches: []Match{
					{Word: "alpha"},
					{Word: "beta"},
					{Word: "gamma"},
				},
			}},
		}
	})
	c.SetOnSelect(func(s string) { got = s })
	c.Render(20)

	// Filtered: ["alpha", "gamma"]. Down → gamma.
	c.HandleInput("\x1b[B")
	if got != "" {
		t.Fatalf("selected before Enter: %q", got)
	}
	c.HandleInput("\r")
	if got != "gamma " {
		t.Errorf("selected %q, want %q (gamma, filtered from 3 items)", got, "gamma ")
	}
}

// TestCompletionsEmptySlotPreservesPrefix guards a regression where selecting a
// completion in an empty token slot (ReplaceStart == ReplaceEnd, e.g. just after
// "/model ") dropped the text before the cursor, turning "/model " + "gpt" into
// "gpt " instead of "/model gpt ".
func TestCompletionsEmptySlotPreservesPrefix(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "/model " {
		input.HandleInput(string(r))
	}
	cur := input.Cursor() // 7, after the trailing space
	var got string
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: cur, ReplaceEnd: cur,
			Groups: []MatchGroup{{Matches: []Match{{Word: "gpt"}}}},
		}
	})
	c.SetOnSelect(func(s string) { got = s })
	c.Render(40)
	c.HandleInput("\r")
	if got != "/model gpt " {
		t.Errorf("selected %q, want %q", got, "/model gpt ")
	}
}

func TestCompletionsEscDismiss(t *testing.T) {
	input := NewLineInput("")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 0,
			Groups: []MatchGroup{{Matches: []Match{{Word: "x"}}}},
		}
	})
	c.Render(20)
	if !c.Visible() {
		t.Fatal("expected visible")
	}
	c.HandleInput("\x1b")
	if c.Visible() {
		t.Fatal("expected hidden after Esc")
	}
}

// TestCompletionsSpaceNoDetailFallsThrough guards a regression where pressing
// space on a visible completion with no onDetail handler leaked the widget lock
// (an early return that skipped Unlock), freezing every subsequent Render. Here
// the space must not be consumed, and a following Render must not block.
func TestCompletionsSpaceNoDetailFallsThrough(t *testing.T) {
	input := NewLineInput("/mcp")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 4,
			Groups: []MatchGroup{{Matches: []Match{{Word: "/mcp"}}}},
		}
	})
	c.Render(40)
	if c.HandleInput(" ") {
		t.Fatal("space should not be consumed when no onDetail is set")
	}
	// Would block forever if the lock had leaked.
	c.Render(40)
}

// TestCompletionsSpaceDetailUnhandledFallsThrough verifies that when onDetail
// declines the item (returns false), the space falls through so it can be
// inserted as text (e.g. "/mcp " to reveal sub-actions).
func TestCompletionsSpaceDetailUnhandledFallsThrough(t *testing.T) {
	input := NewLineInput("/mcp")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 4,
			Groups: []MatchGroup{{Matches: []Match{{Word: "/mcp"}}}},
		}
	})
	c.SetOnDetail(func(string) bool { return false })
	c.Render(40)
	if c.HandleInput(" ") {
		t.Fatal("space should fall through when onDetail returns false")
	}
	c.Render(40) // must not block
}

// TestCompletionsSpaceDetailHandledConsumes verifies that when onDetail handles
// the item (returns true), the space is consumed and not inserted as text.
func TestCompletionsSpaceDetailHandledConsumes(t *testing.T) {
	input := NewLineInput("abc")
	called := false
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 3,
			Groups: []MatchGroup{{Matches: []Match{{Word: "abc"}}}},
		}
	})
	c.SetOnDetail(func(string) bool { called = true; return true })
	c.Render(40)
	if !c.HandleInput(" ") {
		t.Fatal("space should be consumed when onDetail returns true")
	}
	if !called {
		t.Fatal("onDetail was not invoked")
	}
	c.Render(40) // must not block
}

func TestCompletionsClampsToMaxItems(t *testing.T) {
	var items []Match
	for range 20 {
		items = append(items, Match{Word: "item"})
	}
	input := NewLineInput("")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 0,
			Groups: []MatchGroup{{Matches: items}},
		}
	})
	lines := c.Render(80)
	// Render shows up to 10 items.
	if len(lines) > 10 {
		t.Errorf("rendered %d lines, should be <= 10", len(lines))
	}
}

// TestCompletionsEnterUsesOnAccept verifies that Enter invokes the dedicated
// onAccept callback rather than the generic onSelect callback.
func TestCompletionsEnterUsesOnAccept(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "a" {
		input.HandleInput(string(r))
	}
	selectCalls := 0
	acceptCalls := 0
	var accepted string
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 1,
			Groups: []MatchGroup{{Matches: []Match{{Word: "alpha"}, {Word: "beta"}}}},
		}
	})
	c.SetOnSelect(func(string) { selectCalls++ })
	c.SetOnAccept(func(s string) { acceptCalls++; accepted = s })
	c.Render(20)
	c.HandleInput("\r")
	if selectCalls != 0 {
		t.Errorf("onSelect called %d times, want 0 when onAccept is set", selectCalls)
	}
	if acceptCalls != 1 {
		t.Errorf("onAccept called %d times, want 1", acceptCalls)
	}
	if accepted != "alpha " {
		t.Errorf("accepted %q, want %q", accepted, "alpha ")
	}
}

// TestCompletionsEnterOnAcceptCanRenderWithoutDeadlock guards against a
// regression where case "\r" invoked onAccept while c.mu was still held
// (HandleInput unlocks only after the switch). A host's onAccept commonly
// triggers a synchronous re-render (e.g. submitting a command prints to
// scrollback and immediately redraws the frame, which renders this same
// Completions widget) — calling Render from inside that locked callback
// deadlocks on the non-reentrant mutex. Runs HandleInput on a goroutine and
// fails the test if it doesn't return within a short deadline.
func TestCompletionsEnterOnAcceptCanRenderWithoutDeadlock(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "a" {
		input.HandleInput(string(r))
	}
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 1,
			Groups: []MatchGroup{{Matches: []Match{{Word: "alpha"}}}},
		}
	})
	c.SetOnAccept(func(s string) {
		c.Render(20) // simulates a submit triggering a synchronous re-render
	})
	c.Render(20)

	done := make(chan struct{})
	go func() {
		c.HandleInput("\r")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleInput(\"\\r\") deadlocked calling onAccept while c.mu was held")
	}
}

// TestCompletionsEnterFallsBackToOnSelect verifies that Enter still works
// when only onSelect is registered, preserving backward compatibility.
func TestCompletionsEnterFallsBackToOnSelect(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "a" {
		input.HandleInput(string(r))
	}
	var selected string
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 1,
			Groups: []MatchGroup{{Matches: []Match{{Word: "alpha"}}}},
		}
	})
	c.SetOnSelect(func(s string) { selected = s })
	c.Render(20)
	c.HandleInput("\r")
	if selected != "alpha " {
		t.Errorf("selected %q, want %q", selected, "alpha ")
	}
}

// TestCompletionsTabUsesOnSelect verifies that Tab still uses onSelect, not
// onAccept, so Tab can cycle/preview without submitting.
func TestCompletionsTabUsesOnSelect(t *testing.T) {
	input := NewLineInput("")
	for _, r := range "a" {
		input.HandleInput(string(r))
	}
	selectCalls := 0
	acceptCalls := 0
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 1,
			Groups: []MatchGroup{{Matches: []Match{{Word: "alpha"}, {Word: "beta"}}}},
		}
	})
	c.SetOnSelect(func(string) { selectCalls++ })
	c.SetOnAccept(func(string) { acceptCalls++ })
	c.Render(20)
	c.HandleInput("\t")
	if selectCalls != 1 {
		t.Errorf("onSelect called %d times after Tab, want 1", selectCalls)
	}
	if acceptCalls != 0 {
		t.Errorf("onAccept called %d times after Tab, want 0", acceptCalls)
	}
}
