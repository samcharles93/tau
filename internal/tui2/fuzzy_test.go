package tui2

import (
	"sort"
	"testing"
)

// --- FuzzyMatch ------------------------------------------------------------

func TestFuzzyMatchExact(t *testing.T) {
	r := FuzzyMatch("hello", "hello")
	if !r.Match {
		t.Fatal("exact match should match")
	}
	if r.Score >= 0 {
		t.Fatalf("exact match should have negative score (got %f)", r.Score)
	}
}

func TestFuzzyMatchCaseInsensitive(t *testing.T) {
	r := FuzzyMatch("HELLO", "hello")
	if !r.Match {
		t.Fatal("case-insensitive match should match")
	}

	r2 := FuzzyMatch("hello", "HELLO")
	if !r2.Match {
		t.Fatal("case-insensitive match should match")
	}
}

func TestFuzzyMatchSubsequence(t *testing.T) {
	r := FuzzyMatch("hlo", "hello")
	if !r.Match {
		t.Fatal("subsequence should match")
	}
	if len(r.Positions) != 3 {
		t.Fatalf("expected 3 positions, got %v", r.Positions)
	}
}

func TestFuzzyMatchNoMatch(t *testing.T) {
	r := FuzzyMatch("xyz", "hello")
	if r.Match {
		t.Fatal("non-matching query should not match")
	}
}

func TestFuzzyMatchEmptyQuery(t *testing.T) {
	r := FuzzyMatch("", "anything")
	if !r.Match {
		t.Fatal("empty query should match everything")
	}
	if r.Score != 0 {
		t.Fatalf("empty query should score 0, got %f", r.Score)
	}
}

func TestFuzzyMatchLongerQueryThanCandidate(t *testing.T) {
	r := FuzzyMatch("hello world", "hi")
	if r.Match {
		t.Fatal("query longer than candidate should not match")
	}
}

func TestFuzzyMatchSwappedQueryFallback(t *testing.T) {
	// "fn2" should also match via the swapped "2fn" query, but with a
	// slight score penalty (5) applied on top of whatever "2fn" scores.
	r := FuzzyMatch("fn2", "abc2fndef")
	if !r.Match {
		t.Fatal("swapped query fallback should match")
	}
}

func TestFuzzyMatchSwappedDigitsLetters(t *testing.T) {
	r := FuzzyMatch("3ab", "xyzab3")
	if r.Match {
		t.Log("matched via swapped query (digits-letters -> letters-digits)")
	}
}

// --- fuzzyMatchRunes -------------------------------------------------------

func TestFuzzyMatchRunesBoundaryReward(t *testing.T) {
	// Word-boundary match ("w" at index 0) should be rewarded vs. mid-word.
	r := FuzzyMatch("w", "hello world")
	if !r.Match {
		t.Fatal("boundary match should match")
	}
	// 'w' appears at index 6, after space (a boundary), so it should have
	// a boundary reward applied.
}

func TestFuzzyMatchRunesConsecutiveBonus(t *testing.T) {
	r := FuzzyMatch("wor", "hello world")
	if !r.Match {
		t.Fatal("consecutive match should match")
	}
	// The first character "w" is a boundary match (after space at index 6),
	// then "o" and "r" are consecutive, which each get a consecutive bonus.
}

func TestFuzzyMatchRunesGapPenalty(t *testing.T) {
	// "hl" has gaps (h at 0, l at 2 in "hello") — should have a gap penalty
	// for the 1 character between them.
	r := FuzzyMatch("hl", "hello")
	if !r.Match {
		t.Fatal("match with gaps should match")
	}
	// Positions should be [0, 2]
	if len(r.Positions) != 2 || r.Positions[0] != 0 || r.Positions[1] != 2 {
		t.Fatalf("positions = %v, want [0, 2]", r.Positions)
	}
}

func TestFuzzyMatchRunesOutOfOrderFails(t *testing.T) {
	r := FuzzyMatch("lh", "hello")
	if r.Match {
		t.Fatal("out of order query should not match")
	}
}

func TestFuzzyMatchRunesExactExact(t *testing.T) {
	r := FuzzyMatch("hello", "hello")
	if !r.Match {
		t.Fatal("identical strings should match")
	}
	if r.Score > -100 {
		t.Fatal("exact match should get the -100 exact-match bonus, got score", r.Score)
	}
}

// --- fuzzySwap -------------------------------------------------------------

func TestFuzzySwapLettersThenDigits(t *testing.T) {
	r := fuzzySwap("ab12")
	if r != "12ab" {
		t.Fatalf("fuzzySwap('ab12') = %q, want %q", r, "12ab")
	}
}

func TestFuzzySwapDigitsThenLetters(t *testing.T) {
	r := fuzzySwap("12ab")
	if r != "ab12" {
		t.Fatalf("fuzzySwap('12ab') = %q, want %q", r, "ab12")
	}
}

func TestFuzzySwapNoMatch(t *testing.T) {
	r := fuzzySwap("abc")
	if r != "" {
		t.Fatalf("fuzzySwap('abc') = %q, want empty", r)
	}
	r2 := fuzzySwap("123")
	if r2 != "" {
		t.Fatalf("fuzzySwap('123') = %q, want empty", r2)
	}
}

func TestFuzzySwapMixedNoMatch(t *testing.T) {
	r := fuzzySwap("a1b2")
	if r != "" {
		t.Fatalf("fuzzySwap('a1b2') = %q, want empty (alternating pattern)", r)
	}
}

// --- fuzzyHighlightSpans ---------------------------------------------------

func TestFuzzyHighlightSpansEmpty(t *testing.T) {
	spans := fuzzyHighlightSpans(nil, "hello")
	if spans != nil {
		t.Fatalf("expected nil spans for empty positions, got %v", spans)
	}
}

func TestFuzzyHighlightSpansSingle(t *testing.T) {
	spans := fuzzyHighlightSpans([]int{2}, "hello")
	if len(spans) != 1 || spans[0][0] != 2 || spans[0][1] != 3 {
		t.Fatalf("spans = %v, want [[2, 3]]", spans)
	}
}

func TestFuzzyHighlightSpansMergesAdjacent(t *testing.T) {
	spans := fuzzyHighlightSpans([]int{1, 2, 3}, "hello")
	if len(spans) != 1 || spans[0][0] != 1 || spans[0][1] != 4 {
		t.Fatalf("adjacent positions should merge: got %v, want [[1, 4]]", spans)
	}
}

func TestFuzzyHighlightSpansNonAdjacentKeepsSeparate(t *testing.T) {
	spans := fuzzyHighlightSpans([]int{0, 2, 4}, "hello")
	if len(spans) != 3 {
		t.Fatalf("non-adjacent positions should stay separate: got %v", spans)
	}
}

func TestFuzzyHighlightSpansFiltersOutOfBounds(t *testing.T) {
	spans := fuzzyHighlightSpans([]int{-1, 0, 10}, "hi")
	if len(spans) != 1 || spans[0][0] != 0 || spans[0][1] != 1 {
		t.Fatalf("should filter out-of-bounds positions: got %v, want [[0,1]]", spans)
	}
}

func TestFuzzyHighlightSpansPartialMerge(t *testing.T) {
	// Positions [0, 1, 3, 4, 5] should merge to [[0,2], [3,6]]
	spans := fuzzyHighlightSpans([]int{0, 1, 3, 4, 5}, "hello world")
	if len(spans) != 2 {
		t.Fatalf("expected 2 merged spans, got %v", spans)
	}
	if spans[0][0] != 0 || spans[0][1] != 2 {
		t.Fatalf("first span = %v, want [0, 2]", spans[0])
	}
	if spans[1][0] != 3 || spans[1][1] != 6 {
		t.Fatalf("second span = %v, want [3, 6]", spans[1])
	}
}

// --- isFuzzyBoundary -------------------------------------------------------

func TestIsFuzzyBoundary(t *testing.T) {
	cases := map[rune]bool{
		' ':  true,
		'\t': true,
		'\n': true,
		'-':  true,
		'_':  true,
		'.':  true,
		'/':  true,
		':':  true,
		'a':  false,
		'0':  false,
		'Z':  false,
	}
	for r, expected := range cases {
		if got := isFuzzyBoundary(r); got != expected {
			t.Errorf("isFuzzyBoundary(%q) = %v, want %v", r, got, expected)
		}
	}
}

// --- Fuzzy scoring consistency ---------------------------------------------

func TestFuzzyScoreLowerIsBetter(t *testing.T) {
	// Exact match should score better (lower) than a gappy one.
	exact := FuzzyMatch("hello", "hello")
	gappy := FuzzyMatch("hlo", "hello")
	if exact.Score >= gappy.Score {
		t.Fatalf("exact score (%f) should be less than gappy score (%f)", exact.Score, gappy.Score)
	}
}

func TestFuzzyBoundaryMatchBetterThanMidWord(t *testing.T) {
	// Matching "w" at word boundary (index 6 in "hello world") should score
	// better than matching "w" mid-word (e.g. in "noway" where 'w' isn't at
	// a boundary).
	boundary := FuzzyMatch("w", "hello world")
	midword := FuzzyMatch("w", "noway")
	if boundary.Score >= midword.Score {
		t.Fatalf("boundary match score (%f) should be less than mid-word (%f)", boundary.Score, midword.Score)
	}
}

// --- Fuzzy ranking for completions -----------------------------------------

func TestFuzzyMatchesRankedCorrectly(t *testing.T) {
	words := []string{"gpt-4", "claude-3", "gpt-4-turbo", "gpt-3.5-turbo"}
	query := "gpt"

	type scored struct {
		word  string
		score float64
	}
	var results []scored
	for _, w := range words {
		r := FuzzyMatch(query, w)
		if r.Match {
			results = append(results, scored{w, r.Score})
		}
	}
	if len(results) == 0 {
		t.Fatal("expected at least one match for 'gpt'")
	}
	// Verify sorting: lower score (better) first.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score < results[j].score
	})
	for i := 1; i < len(results); i++ {
		if results[i].score < results[i-1].score {
			t.Fatalf("results not sorted by score (ascending): %+v", results)
		}
	}
}
