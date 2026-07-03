package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// ── Data model ───────────────────────────────────────────────────────────────

type Match struct {
	Word        string
	Display     string
	Description string
}

type MatchGroup struct {
	Title           string
	Matches         []Match
	NoTrailingSpace bool
}

type CompletionSet struct {
	Groups       []MatchGroup
	ReplaceStart int
	ReplaceEnd   int
}

type CompletionContext struct {
	Text   string
	Cursor int
}

type CompletionProvider func(ctx CompletionContext) *CompletionSet

// Completion is an alias for Match (backward compat).
type Completion = Match

func SplitLastWord(s string) (prefix, last string) {
	runes := []rune(s)
	i := len(runes) - 1
	for i >= 0 && runes[i] == ' ' {
		i--
	}
	end := i + 1
	for i >= 0 && runes[i] != ' ' {
		i--
	}
	if i < 0 {
		return "", string(runes[:end])
	}
	return string(runes[:i+1]), string(runes[i+1 : end])
}

// ── Component ────────────────────────────────────────────────────────────────

type Completions struct {
	provider CompletionProvider
	input    *LineInput
	onSelect func(string)
	// onDetail is invoked when the user presses space on a selected item. It
	// returns whether it handled the item; if it returns false the space is not
	// consumed, so it falls through and is inserted as ordinary text.
	onDetail func(string) bool

	mu       sync.Mutex
	rawSet   *CompletionSet
	filtered []filteredRow
	groups   []matchedGroup
	selected int
	visible  bool
}

type filteredRow struct {
	match    Match
	groupIdx int
	score    float64
	noSpace  bool
	hl       [][2]int
	text     string
}

type matchedGroup struct {
	title string
	start int
	end   int
}

func NewCompletions(input *LineInput, provider CompletionProvider) *Completions {
	return &Completions{input: input, provider: provider}
}

func (c *Completions) SetOnSelect(fn func(string))      { c.onSelect = fn }
func (c *Completions) SetOnDetail(fn func(string) bool) { c.onDetail = fn }
func (c *Completions) Invalidate()                      {}

func (c *Completions) HandleInput(data string) bool {
	c.mu.Lock()
	if !c.visible {
		c.mu.Unlock()
		return false
	}
	var chosen string
	detail := ""
	choose := false
	wantDetail := false
	consumed := true
	n := len(c.filtered)

	switch data {
	case "\x1b[A", "\x1bOA", "\x10":
		if c.selected > 0 {
			c.selected--
		}
	case "\x1b[B", "\x1bOB", "\x0e":
		if c.selected < n-1 {
			c.selected++
		}
	case "\t":
		if n == 0 {
			consumed = false
			break
		}
		lcp := c.filtered[0].text
		for i := 1; i < n; i++ {
			lcp = commonPrefix(lcp, c.filtered[i].text)
		}
		query := c.currentQuery()
		// If LCP extends further than the current query, extend to it.
		// Otherwise insert the best match (index 0) — this is what the user
		// expects from the first Tab press. Arrow keys handle cycling.
		if lcp != query && len([]rune(lcp)) > len([]rune(query)) {
			c.extendQueryLocked(lcp)
			c.mu.Unlock()
			return true
		}
		c.selected = 0
		chosen = c.fullReplace(c.filtered[0])
		choose = true
	case "\x1b[Z": // Shift+Tab — insert the last match (bottom of list)
		if n > 0 {
			c.selected = n - 1
			chosen = c.fullReplace(c.filtered[c.selected])
			choose = true
		}
	case "\r":
		if c.selected >= 0 && c.selected < n {
			chosen = c.fullReplace(c.filtered[c.selected])
			choose = true
		}
	case "\x1b":
		c.hideLocked()

	case " ":
		// Space on a selected completion item defers to the detail callback
		// (used to show session info). The callback runs after the lock is
		// released; whether the space is consumed depends on its result.
		if c.onDetail != nil && c.selected >= 0 && c.selected < n {
			detail = c.filtered[c.selected].match.Word
			wantDetail = true
			break
		}
		consumed = false

	default:
		consumed = false
	}
	c.mu.Unlock()

	if wantDetail {
		// If the callback handled the item, consume the space; otherwise let it
		// fall through so it is inserted as text (e.g. "/mcp " to reveal
		// sub-actions).
		return c.onDetail(detail)
	}
	if choose && c.onSelect != nil {
		c.onSelect(chosen)
	}
	return consumed
}

func (c *Completions) fullReplace(row filteredRow) string {
	if c.rawSet == nil {
		return row.match.Word
	}
	text := c.input.Value()
	// Keep everything before the replacement span. Guard only on ReplaceStart
	// being positive — an empty token slot (ReplaceStart == ReplaceEnd, e.g.
	// just after "/model ") must still preserve the text up to the cursor.
	prefix := ""
	if c.rawSet.ReplaceStart > 0 {
		prefix = runePrefix(text, c.rawSet.ReplaceStart)
	}
	result := prefix + row.match.Word
	if !row.noSpace {
		result += " "
	}
	return result
}

func commonPrefix(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return string(ra[:n])
}

func (c *Completions) currentQuery() string {
	if c.rawSet == nil {
		return ""
	}
	t := c.input.Value()
	rs := []rune(t)
	s := max(0, min(c.rawSet.ReplaceStart, len(rs)))
	e := max(s, min(c.rawSet.ReplaceEnd, len(rs)))
	return string(rs[s:e])
}

func (c *Completions) extendQueryLocked(lcp string) {
	if c.rawSet == nil {
		return
	}
	t := c.input.Value()
	rs := []rune(t)
	s := max(0, min(c.rawSet.ReplaceStart, len(rs)))
	e := max(s, min(c.rawSet.ReplaceEnd, len(rs)))
	newText := string(rs[:s]) + lcp + string(rs[e:])
	c.input.SetValueAndCursor(newText, s+runeLen(lcp))
}

func (c *Completions) hideLocked() {
	c.visible = false
	c.filtered = nil
	c.groups = nil
	c.rawSet = nil
	c.selected = 0
}

func (c *Completions) Visible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.visible
}

func (c *Completions) Render(width int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refreshLocked()
	if !c.visible || len(c.filtered) == 0 {
		return nil
	}

	var out []string
	colour := termkit.ColorEnabled()

	// Scroll a fixed-size window over the matches so the selected row stays
	// visible — without this, selections past the 10th item are invisible and
	// unreachable. Centre the window on the selection, clamped to the ends.
	const window = 10
	n := len(c.filtered)
	size := min(n, window)
	start := max(c.selected-size/2, 0)
	if start+size > n {
		start = n - size
	}
	end := start + size

	// Align descriptions into a column: pad each word to the widest word that
	// has a description (capped so very long words don't push the column off
	// screen). Computed across all filtered rows so the column stays put while
	// scrolling.
	descCol := c.descColumn()

	for i := start; i < end; i++ {
		row := c.filtered[i]
		if g := c.groupOf(i); g != nil && g.start == i && g.title != "" {
			out = append(out, dim(g.title, colour))
		}
		out = append(out, c.renderRow(i == c.selected, row.text, row.match.Description, row.hl, colour, descCol))
	}
	for i := range out {
		out[i] = truncateANSIToWidth(out[i], width)
	}
	return out
}

func (c *Completions) groupOf(idx int) *matchedGroup {
	for i := range c.groups {
		if c.groups[i].start <= idx && idx < c.groups[i].end {
			return &c.groups[i]
		}
	}
	return nil
}

func dim(s string, colour bool) string {
	if colour {
		return termkit.Wrap(s, termkit.Dim)
	}
	return s
}

// descColumn returns the width to pad words to so descriptions line up in a
// column. It is the widest word among filtered rows that carry a description,
// capped by descColumnMax. Rows without descriptions don't influence it.
func (c *Completions) descColumn() int {
	const descColumnMax = 44
	col := 0
	for i := range c.filtered {
		if c.filtered[i].match.Description == "" {
			continue
		}
		if w := VisibleWidth(c.filtered[i].text); w > col {
			col = w
		}
	}
	if col > descColumnMax {
		col = descColumnMax
	}
	return col
}

func (c *Completions) renderRow(selected bool, word, desc string, hl [][2]int, colour bool, descCol int) string {
	prefix := "  "
	if !colour {
		if selected {
			return "› " + word + descGutter(word, desc, descCol)
		}
		return prefix + word + descGutter(word, desc, descCol)
	}
	// Build word with bold highlights on matched spans.
	body := word
	if len(hl) > 0 {
		var sb strings.Builder
		runes := []rune(word)
		hi := 0
		for i := range runes {
			for hi < len(hl) && i >= hl[hi][1] {
				hi++
			}
			inHL := hi < len(hl) && i >= hl[hi][0] && i < hl[hi][1]
			ch := string(runes[i])
			if inHL {
				sb.WriteString(termkit.Style(ch, termkit.Bold))
			} else {
				sb.WriteString(ch)
			}
		}
		body = sb.String()
	}
	chevron := "  "
	if selected {
		chevron = "▶ "
	}
	body = termkit.FgOnly(chevron+body, termkit.ColorGrey)
	if desc != "" {
		// Pad outside the highlight so the selection bar stays tight to the
		// word while descriptions still line up in a column.
		body += padTo(word, descCol)
		body += termkit.FgOnly(" "+desc, termkit.ColorGrey)
	}
	return body
}

// padTo returns the spaces needed to pad word's visible width up to col, or ""
// if the word already meets or exceeds it (an over-long outlier).
func padTo(word string, col int) string {
	gap := col - VisibleWidth(word)
	if gap < 1 {
		return ""
	}
	return strings.Repeat(" ", gap)
}

// descGutter aligns desc into the description column for the no-colour render
// path: pad the word to the column, then a one-space gutter, then desc. Empty
// when there is no description.
func descGutter(word, desc string, col int) string {
	if desc == "" {
		return ""
	}
	return padTo(word, col) + " " + desc
}

// ── refresh ──────────────────────────────────────────────────────────────────

func (c *Completions) refreshLocked() {
	if c.provider == nil || c.input == nil {
		c.hideLocked()
		return
	}
	set := c.provider(CompletionContext{
		Text:   c.input.Value(),
		Cursor: c.input.Cursor(),
	})
	if set == nil || len(set.Groups) == 0 {
		c.hideLocked()
		return
	}
	textRunes := []rune(c.input.Value())
	rs := max(0, min(set.ReplaceStart, len(textRunes)))
	re := max(rs, min(set.ReplaceEnd, len(textRunes)))
	query := ""
	if rs < re {
		query = string(textRunes[rs:re])
	}

	type raw struct {
		match   Match
		group   int
		noSpace bool
	}
	var all []raw
	for gi, g := range set.Groups {
		for _, m := range g.Matches {
			all = append(all, raw{m, gi, g.NoTrailingSpace})
		}
	}

	filtered := fuzzyFilterSlice(all, query, func(r raw) string { return r.match.Word })

	c.rawSet = set
	c.filtered = c.filtered[:0]
	c.groups = c.groups[:0]

	groupStart := 0
	lastGroup := -1
	for _, fr := range filtered {
		display := fr.match.Display
		if display == "" {
			display = fr.match.Word
		}
		result := FuzzyMatch(query, fr.match.Word)
		hl := queryPositionsToSpans(result.Positions, fr.match.Word)

		row := filteredRow{
			match:    fr.match,
			groupIdx: fr.group,
			score:    result.Score,
			noSpace:  fr.noSpace,
			hl:       hl,
			text:     display,
		}
		c.filtered = append(c.filtered, row)

		if fr.group != lastGroup {
			if lastGroup >= 0 {
				c.groups[len(c.groups)-1].end = len(c.filtered) - 1
			}
			c.groups = append(c.groups, matchedGroup{
				title: set.Groups[fr.group].Title,
				start: groupStart,
			})
			lastGroup = fr.group
		}
		groupStart++
	}
	if lastGroup >= 0 && len(c.groups) > 0 {
		c.groups[len(c.groups)-1].end = len(c.filtered)
	}

	c.visible = len(c.filtered) > 0
	if c.selected >= len(c.filtered) {
		c.selected = 0
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func runeLen(s string) int { return len([]rune(s)) }

func runePrefix(s string, n int) string {
	runes := []rune(s)
	if n > len(runes) {
		n = len(runes)
	}
	return string(runes[:n])
}

func fuzzyFilterSlice[T any](items []T, query string, getText func(T) string) []T {
	tokens := splitFields(query)
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
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].score < out[j-1].score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	result := make([]T, len(out))
	for i, s := range out {
		result[i] = s.item
	}
	return result
}

func queryPositionsToSpans(positions []int, word string) [][2]int {
	if len(positions) == 0 {
		return nil
	}
	var spans [][2]int
	for _, p := range positions {
		if p >= 0 && p < runeLen(word) {
			spans = append(spans, [2]int{p, p + 1})
		}
	}
	if len(spans) <= 1 {
		return spans
	}
	merged := [][2]int{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s[0] == last[1] {
			last[1] = s[1]
		} else {
			merged = append(merged, s)
		}
	}
	return merged
}

func splitFields(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}
