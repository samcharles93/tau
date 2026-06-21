package taui

import (
	"regexp"
	"sort"
	"strings"
)

// Fuzzy matching, ported from Pi's fuzzy.ts (MIT, © 2025 Mario Zechner).
// A query matches if all its characters appear in order within the candidate
// (not necessarily consecutively). Lower score = better match. The matched rune
// positions are tracked so callers can highlight them.

// FuzzyResult is the outcome of matching a query against a candidate string.
type FuzzyResult struct {
	Match     bool
	Score     float64 // lower is better
	Positions []int   // rune indices in the candidate that matched the query
}

var (
	fuzzyLettersDigits = regexp.MustCompile(`^([a-z]+)([0-9]+)$`)
	fuzzyDigitsLetters = regexp.MustCompile(`^([0-9]+)([a-z]+)$`)
)

func isFuzzyBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '-', '_', '.', '/', ':':
		return true
	}
	return false
}

// FuzzyMatch reports whether query matches text (case-insensitive, in order),
// with a quality score (lower is better) and the matched rune positions.
func FuzzyMatch(query, text string) FuzzyResult {
	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(text))

	primary := fuzzyMatchRunes(q, t)
	if primary.Match {
		return primary
	}

	// Fallback: swap a letters+digits query to digits+letters (and vice versa),
	// e.g. "fn2" also tries "2fn". Slight score penalty so direct wins.
	swapped := fuzzySwap(string(q))
	if swapped == "" {
		return primary
	}
	s := fuzzyMatchRunes([]rune(swapped), t)
	if !s.Match {
		return primary
	}
	s.Score += 5
	return s
}

// fuzzyMatchRunes greedily matches q within t (both already lowercased).
func fuzzyMatchRunes(q, t []rune) FuzzyResult {
	if len(q) == 0 {
		return FuzzyResult{Match: true, Score: 0}
	}
	if len(q) > len(t) {
		return FuzzyResult{}
	}

	qi := 0
	score := 0.0
	last := -1
	consecutive := 0
	positions := make([]int, 0, len(q))

	for i := 0; i < len(t) && qi < len(q); i++ {
		if t[i] != q[qi] {
			continue
		}
		boundary := i == 0 || isFuzzyBoundary(t[i-1])

		if last == i-1 {
			consecutive++
			score -= float64(consecutive) * 5
		} else {
			consecutive = 0
			if last >= 0 {
				score += float64(i-last-1) * 2 // penalize gaps
			}
		}
		if boundary {
			score -= 10 // reward word-boundary matches
		}
		score += float64(i) * 0.1 // slight penalty for later matches

		last = i
		positions = append(positions, i)
		qi++
	}

	if qi < len(q) {
		return FuzzyResult{}
	}
	if string(q) == string(t) {
		score -= 100 // exact match
	}
	return FuzzyResult{Match: true, Score: score, Positions: positions}
}

// fuzzySwap returns the alphanumeric-swapped form of q ("ab12" -> "12ab"), or ""
// if q isn't a pure letters+digits / digits+letters string.
func fuzzySwap(q string) string {
	if m := fuzzyLettersDigits.FindStringSubmatch(q); m != nil {
		return m[2] + m[1]
	}
	if m := fuzzyDigitsLetters.FindStringSubmatch(q); m != nil {
		return m[2] + m[1]
	}
	return ""
}

// FuzzyFilter returns items whose text fuzzy-matches the query, best first.
// Space-separated tokens are all required (AND). Empty query returns items
// unchanged.
func FuzzyFilter[T any](items []T, query string, getText func(T) string) []T {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return items
	}

	type scored struct {
		item  T
		score float64
	}
	var out []scored
	for _, it := range items {
		text := getText(it)
		total := 0.0
		ok := true
		for _, tok := range tokens {
			m := FuzzyMatch(tok, text)
			if !m.Match {
				ok = false
				break
			}
			total += m.Score
		}
		if ok {
			out = append(out, scored{it, total})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score < out[j].score })

	result := make([]T, len(out))
	for i, s := range out {
		result[i] = s.item
	}
	return result
}
