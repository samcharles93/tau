package taui

import "strings"

// tableColSep separates columns; its visible width (3) matches the "─┼─"
// separator used in the header rule, so columns line up.
const tableColSep = " │ "

// Table renders a header row, a separator rule, and body rows, with each
// column padded to its widest cell. If the natural width exceeds the render
// width, columns are shrunk proportionally and cells truncated to fit.
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable creates a Table component.
func NewTable(headers []string, rows [][]string) *Table {
	return &Table{headers: headers, rows: rows}
}

// Invalidate is a no-op; Table holds no cached render.
func (t *Table) Invalidate() {}

// Render returns the header, separator, and body lines.
func (t *Table) Render(width int) []string {
	cols := len(t.headers)
	for _, r := range t.rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return nil
	}

	colWidths := make([]int, cols)
	measure := func(cells []string) {
		for i, c := range cells {
			if i >= cols {
				continue
			}
			if w := VisibleWidth(c); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}
	measure(t.headers)
	for _, r := range t.rows {
		measure(r)
	}

	sepWidth := VisibleWidth(tableColSep) * max(0, cols-1)
	natural := 0
	for _, w := range colWidths {
		natural += w
	}
	if width > 0 && natural+sepWidth > width && natural > 0 {
		avail := max(cols, width-sepWidth)
		for i, w := range colWidths {
			scaled := int(float64(w) * float64(avail) / float64(natural))
			colWidths[i] = max(3, scaled)
		}
	}

	lines := make([]string, 0, len(t.rows)+2)
	lines = append(lines, formatTableRow(t.headers, colWidths))
	rule := make([]string, cols)
	for i, w := range colWidths {
		rule[i] = strings.Repeat("─", w)
	}
	lines = append(lines, strings.Join(rule, "─┼─"))
	for _, r := range t.rows {
		lines = append(lines, formatTableRow(r, colWidths))
	}
	return lines
}

func formatTableRow(cells []string, colWidths []int) string {
	parts := make([]string, len(colWidths))
	for i, w := range colWidths {
		var c string
		if i < len(cells) {
			c = cells[i]
		}
		c = TruncateANSIToWidth(c, w, "…")
		if pad := w - VisibleWidth(c); pad > 0 {
			c += strings.Repeat(" ", pad)
		}
		parts[i] = c
	}
	return strings.Join(parts, tableColSep)
}
