package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

func TestTauGlamourConfigHasNoBackgrounds(t *testing.T) {
	cfg := tauGlamourConfig()

	// Check all chroma entries have no background set.
	if cfg.CodeBlock.Chroma == nil {
		t.Fatal("expected CodeBlock.Chroma to be non-nil")
	}
	c := cfg.CodeBlock.Chroma

	entries := []ansi.StylePrimitive{
		c.Text, c.Error, c.Comment, c.CommentPreproc,
		c.Keyword, c.KeywordReserved, c.KeywordNamespace, c.KeywordType,
		c.Operator, c.Punctuation,
		c.Name, c.NameBuiltin, c.NameTag, c.NameAttribute,
		c.NameClass, c.NameConstant, c.NameDecorator, c.NameException,
		c.NameFunction, c.NameOther,
		c.Literal, c.LiteralNumber, c.LiteralDate,
		c.LiteralString, c.LiteralStringEscape,
		c.GenericDeleted, c.GenericEmph, c.GenericInserted,
		c.GenericStrong, c.GenericSubheading,
		c.Background,
	}
	for _, e := range entries {
		if e.BackgroundColor != nil {
			t.Errorf("chroma StylePrimitive has background color set: %+v", e)
		}
	}

	// Check main style config has no hard-coded background on code blocks.
	if cfg.CodeBlock.BackgroundColor != nil {
		t.Errorf("CodeBlock has background color set")
	}

	// Document and Paragraph should have no background.
	if cfg.Document.BackgroundColor != nil {
		t.Errorf("Document has background color set")
	}
}

func TestHighlightKnownLanguages(t *testing.T) {
	snippets := map[string]string{
		"go":         "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
		"python":     "def greet(name):\n    return f\"Hello, {name}!\"\n",
		"bash":       "#!/bin/bash\necho \"hello world\"\n",
		"json":       "{\n  \"key\": \"value\",\n  \"num\": 42\n}\n",
		"yaml":       "name: example\nitems:\n  - one\n  - two\n",
		"javascript": "function greet(name) {\n  return `Hello, ${name}!`;\n}\n",
		"rust":       "fn main() {\n    println!(\"hello\");\n}\n",
		"csharp":     "class Program {\n    static void Main() {\n        System.Console.WriteLine(\"hello\");\n    }\n}\n",
		"powershell": "Write-Host \"hello\"\n",
	}

	for lang, code := range snippets {
		t.Run(lang, func(t *testing.T) {
			// Build a renderer with the Tau config at a reasonable width.
			r, err := glamour.NewTermRenderer(
				glamour.WithWordWrap(80),
				glamour.WithStyles(tauGlamourConfig()),
			)
			if err != nil {
				t.Fatalf("failed to create renderer: %v", err)
			}

			md := "```" + lang + "\n" + code + "\n```\n"
			out, err := r.Render(md)
			if err != nil {
				t.Fatalf("unexpected error rendering %s: %v", lang, err)
			}

			// The output should contain ANSI escape codes (syntax highlighting).
			if !strings.Contains(out, "\x1b[") {
				t.Errorf("expected ANSI escape codes in rendered output for %s, got raw text:\n%s", lang, out)
			}

			// The output should still contain the original code content
			// (stripped of ANSI).
			plain := stripANSI(out)
			if lang == "go" && !strings.Contains(plain, "func main()") {
				t.Errorf("rendered output missing expected code content for %s:\n%s", lang, plain)
			}
		})
	}
}

func TestHighlightUnknownLanguage(t *testing.T) {
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(80),
		glamour.WithStyles(tauGlamourConfig()),
	)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	md := "```xyz-nonsense\nsome code here\nmore code\n```\n"
	out, err := r.Render(md)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should render without crashing and contain the code text.
	plain := stripANSI(out)
	if !strings.Contains(plain, "some code here") {
		t.Errorf("output missing code content for unknown language:\n%s", plain)
	}
}

func TestHighlightOmittedLanguage(t *testing.T) {
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(80),
		glamour.WithStyles(tauGlamourConfig()),
	)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	md := "```\nsome code here\nmore code\n```\n"
	out, err := r.Render(md)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	plain := stripANSI(out)
	if !strings.Contains(plain, "some code here") {
		t.Errorf("output missing code content for omitted language:\n%s", plain)
	}
}

func TestHighlightIncompleteFence(t *testing.T) {
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(80),
		glamour.WithStyles(tauGlamourConfig()),
	)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	// Unclosed fence — the opening ```go is present but no closing ``` .
	md := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	out, err := r.Render(md)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should render without crashing.
	plain := stripANSI(out)
	if !strings.Contains(plain, "func main()") {
		t.Errorf("output missing code content for incomplete fence:\n%s", plain)
	}
}

func TestPersistedContentIsPlain(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Simulate a completed assistant response with a code block.
	m.streaming = "Here is some Go:\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nThat's it."
	m.handleChatEvent(tauchat.ChatResponseCompletedEvent{})
	// finalizeResponse is called internally; it stores raw in lastAssistantText
	// and ANSI-styled in renderedLines.

	if m.lastAssistantText == "" {
		t.Fatal("expected non-empty lastAssistantText")
	}
	if strings.Contains(m.lastAssistantText, "\x1b[") {
		t.Error("lastAssistantText contains ANSI escapes; persisted content must be plain")
	}
	if !strings.Contains(m.lastAssistantText, "```go") {
		t.Error("lastAssistantText missing fenced code block")
	}
}

func TestTauGlamourConfigRendersHeadings(t *testing.T) {
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(80),
		glamour.WithStyles(tauGlamourConfig()),
	)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}

	out, err := r.Render("# Main Heading\n\nSome paragraph text.\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "\x1b[") {
		t.Fatal("expected ANSI escape codes in rendered markdown")
	}

	// The heading should be styled with the accent colour, not plain.
	plain := stripANSI(out)
	if !strings.Contains(plain, "Main Heading") {
		t.Errorf("rendered output missing heading text:\n%s", plain)
	}
}

func TestTauChromaStyleMapsAllTokenTypes(t *testing.T) {
	c := tauChromaInline()

	// Spot-check that every required token type has a color set.
	required := map[string]*string{
		"Text":              c.Text.Color,
		"Error":             c.Error.Color,
		"Comment":           c.Comment.Color,
		"Keyword":           c.Keyword.Color,
		"KeywordReserved":   c.KeywordReserved.Color,
		"Name":              c.Name.Color,
		"NameFunction":      c.NameFunction.Color,
		"LiteralString":     c.LiteralString.Color,
		"LiteralNumber":     c.LiteralNumber.Color,
		"GenericSubheading": c.GenericSubheading.Color,
	}
	for name, col := range required {
		if col == nil {
			t.Errorf("Chroma.%s.Color is nil", name)
		}
	}

	// Background must be nil.
	if c.Background.Color != nil {
		t.Error("Chroma.Background.Color must be nil")
	}
	if c.Background.BackgroundColor != nil {
		t.Error("Chroma.Background.BackgroundColor must be nil")
	}
}

func TestHexOutput(t *testing.T) {
	tests := []struct {
		c    [3]uint8
		want string
	}{
		{theme.AccentColor, "#D19A66"},
		{theme.ToneMuted, "#808696"},
		{theme.TonePrimary, "#FFFFFF"},
		{theme.ToneInfo, "#78AAFF"},
		{theme.ToneSuccess, "#8CDC8C"},
		{theme.ToneWarn, "#FFC878"},
		{theme.ToneError, "#FFA0A0"},
		{theme.ToneBody, "#CCCCCC"},
		{[3]uint8{0, 0, 0}, "#000000"},
		{[3]uint8{0x0A, 0x1B, 0x2C}, "#0A1B2C"},
	}
	for _, tt := range tests {
		got := hex(tt.c)
		if got != tt.want {
			t.Errorf("hex(%v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}
