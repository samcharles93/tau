package tui2

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/theme"
)

func BenchmarkRendererPipeline(b *testing.B) {
	const width = 120
	for _, size := range []int{4 << 10, 16 << 10, 64 << 10} {
		payload := rendererBenchmarkPayload(size)
		plainPayload := rendererPlainBenchmarkPayload(size)
		name := fmt.Sprintf("%dKiB", size>>10)

		b.Run(name+"/glamour", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			md := "```result.md\n" + payload + "\n```"
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Render(md); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(name+"/glamour-raw", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Render(payload); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(name+"/lipgloss-inner", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			rendered, err := r.Render("```result.md\n" + payload + "\n```")
			if err != nil {
				b.Fatal(err)
			}
			style := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SecondaryColor)).
				Width(width-8).
				Padding(0, 1)
			b.ReportAllocs()
			for b.Loop() {
				_ = style.Render(strings.TrimRight(rendered, "\n"))
			}
		})

		b.Run(name+"/lipgloss-outer", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			rendered, err := r.Render("```result.md\n" + payload + "\n```")
			if err != nil {
				b.Fatal(err)
			}
			inner := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SecondaryColor)).
				Width(width-8).
				Padding(0, 1).
				Render(strings.TrimRight(rendered, "\n"))
			content := "⠋ read running\n\n" + inner
			style := toolBoxExpandedStyle.Width(width).Padding(0, 1)
			b.ReportAllocs()
			for b.Loop() {
				_ = style.Render(content)
			}
		})

		b.Run(name+"/cached-box-dynamic-header", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			rendered, err := r.Render("```result.md\n" + payload + "\n```")
			if err != nil {
				b.Fatal(err)
			}
			inner := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SecondaryColor)).
				Width(width-8).
				Padding(0, 1).
				Render(strings.TrimRight(rendered, "\n"))
			style := toolBoxExpandedStyle.Width(width).Padding(0, 1)
			full := style.Render("placeholder\n\n" + inner)
			body := rendererBenchmarkAfterLines(full, 2)
			b.ReportAllocs()
			for b.Loop() {
				header := rendererBenchmarkFirstLines(style.Render("⠋ read running (1.2s)"), 2)
				_ = header + body
			}
		})

		b.Run(name+"/cached-box-static", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			tool := toolState{id: "bench", name: "read", status: "done", result: payload}
			full := m.renderToolBox(tool, true, 1, width)
			b.ReportAllocs()
			for b.Loop() {
				_ = full
			}
		})

		b.Run(name+"/reasoning", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = renderReasoningLines(payload, width)
			}
		})

		b.Run(name+"/tool-box-markdown-cold", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			tool := toolState{id: "bench", name: "read", status: "running", result: payload}
			b.ReportAllocs()
			for b.Loop() {
				clear(m.expandedToolCache)
				m.expandedToolCacheOrder = m.expandedToolCacheOrder[:0]
				tool.spinnerIdx++
				_ = m.renderToolBox(tool, true, 1, width)
			}
		})

		b.Run(name+"/tool-box-markdown-warm", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			tool := toolState{id: "bench", name: "read", status: "running", result: payload}
			_ = m.renderToolBox(tool, true, 1, width)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tool.spinnerIdx++
				_ = m.renderToolBox(tool, true, 1, width)
			}
		})

		b.Run(name+"/tool-box-plain-cold", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			tool := toolState{id: "bench", name: "read", status: "running", result: plainPayload}
			b.ReportAllocs()
			for b.Loop() {
				clear(m.expandedToolCache)
				m.expandedToolCacheOrder = m.expandedToolCacheOrder[:0]
				tool.spinnerIdx++
				_ = m.renderToolBox(tool, true, 1, width)
			}
		})

		b.Run(name+"/tool-box-plain-warm", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			tool := toolState{id: "bench", name: "read", status: "running", result: plainPayload}
			_ = m.renderToolBox(tool, true, 1, width)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tool.spinnerIdx++
				_ = m.renderToolBox(tool, true, 1, width)
			}
		})

		b.Run(name+"/full-view-expanded-cold", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.width, m.height = width, 40
			m.tools = []toolState{{id: "bench", name: "read", status: "running", result: payload}}
			m.expandedID = "bench"
			b.ReportAllocs()
			for b.Loop() {
				clear(m.expandedToolCache)
				m.expandedToolCacheOrder = m.expandedToolCacheOrder[:0]
				m.tools[0].spinnerIdx++
				_ = m.View()
			}
		})

		b.Run(name+"/full-view-expanded-warm", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.width, m.height = width, 40
			m.tools = []toolState{{id: "bench", name: "read", status: "running", result: payload}}
			m.expandedID = "bench"
			_ = m.View()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.tools[0].spinnerIdx++
				_ = m.View()
			}
		})

		b.Run(name+"/full-view-collapsed", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.width, m.height = width, 40
			m.tools = []toolState{{id: "bench", name: "read", status: "running", result: payload}}
			b.ReportAllocs()
			for b.Loop() {
				m.tools[0].spinnerIdx++
				_ = m.View()
			}
		})

		b.Run(name+"/full-view-reasoning", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			m.width, m.height = width, 40
			m.showReasoning = true
			m.reasoning = plainPayload
			b.ReportAllocs()
			for b.Loop() {
				m.spinnerFrame++
				_ = m.View()
			}
		})
	}
}

func rendererBenchmarkPayload(size int) string {
	const line = "- item with `inline code` and **emphasis** plus enough text to wrap cleanly\n"
	return strings.Repeat(line, size/len(line)+1)[:size]
}

func rendererPlainBenchmarkPayload(size int) string {
	const line = "plain output text with no markup tokens\n"
	return strings.Repeat(line, size/len(line)+1)[:size]
}

func rendererBenchmarkAfterLines(s string, n int) string {
	for range n {
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			return ""
		}
		s = s[idx+1:]
	}
	return "\n" + s
}

func rendererBenchmarkFirstLines(s string, n int) string {
	end := 0
	for range n {
		idx := strings.IndexByte(s[end:], '\n')
		if idx < 0 {
			return s
		}
		end += idx + 1
	}
	return strings.TrimSuffix(s[:end], "\n")
}
