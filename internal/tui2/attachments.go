package tui2

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// maxAttachmentBytes caps a single pasted image. Larger payloads are skipped
// (left as pasted text) and reported via a notification - multi-megapixel
// screenshots are usually well under this, while uncompressed bitmaps can
// blow past it.
const maxAttachmentBytes = 20 << 20 // 20 MiB

// inputAttachment is a pasted image held by the composer, referenced inline
// in m.input via its placeholder token ("[Image #1]").
type inputAttachment struct {
	id          int
	placeholder string
	mediaType   string
	data        []byte
	name        string
}

func (a inputAttachment) chatAttachment() tauchat.ChatAttachment {
	return tauchat.ChatAttachment{
		Placeholder: a.placeholder,
		MediaType:   a.mediaType,
		Data:        a.data,
		Name:        a.name,
	}
}

// queuedTurn is a prompt waiting behind a running turn; attachments ride
// along so a queued image prompt still sends its images.
type queuedTurn struct {
	text        string
	attachments []tauchat.ChatAttachment
}

// liveAttachments resolves the composer's attachments against text (the
// trimmed input at submit time, or m.input at render time): only attachments
// whose placeholder still appears in the text are live - deleting the
// placeholder detaches the image. Ordered by first appearance in the text.
func (m *model) liveAttachments(text string) []tauchat.ChatAttachment {
	if len(m.attachments) == 0 {
		return nil
	}
	all := make([]tauchat.ChatAttachment, 0, len(m.attachments))
	for _, a := range m.attachments {
		all = append(all, a.chatAttachment())
	}
	return tauchat.ResolveImageAttachments(text, all)
}

// imageGateNotification returns a user-facing message when the submission
// carries image attachments but the currently selected model is known not to
// accept image input - the submit should be blocked and the composer left
// intact. Returns "" when the submit may proceed; when the model's input
// modalities are unknown (live-discovered models, older snapshots) the gate
// fails open and allows the submit.
func (m *model) imageGateNotification(attachments []tauchat.ChatAttachment) string {
	hasImage := false
	for _, a := range attachments {
		if a.IsImage() {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return ""
	}
	ref := m.currentModelRef()
	if supported, known := ref.Config.SupportsInputModality("image"); known && !supported {
		return fmt.Sprintf("model %s doesn't accept images - pick a vision model with /model", ref.ID)
	}
	return ""
}

// --- paste detection --------------------------------------------------------

// pastedImage is a decoded image payload found in pasted content.
type pastedImage struct {
	data      []byte
	mediaType string
	name      string // original file base name when pasted as a path
}

// pasteSegment is one piece of a paste: either literal text or an image.
type pasteSegment struct {
	text  string
	image *pastedImage
}

type pasteParseResult struct {
	segments        []pasteSegment
	found           bool
	skippedOversize int
}

// matchAt reports whether data has lit at byte offset off.
func matchAt(data []byte, off int, lit string) bool {
	return len(data) >= off+len(lit) && string(data[off:off+len(lit)]) == lit
}

// sniffImageMediaType returns the media type when data starts with a known
// image magic number, else "".
func sniffImageMediaType(data []byte) string {
	switch {
	case matchAt(data, 0, "\x89PNG\r\n\x1a\n"):
		return "image/png"
	case matchAt(data, 0, "\xff\xd8\xff"):
		return "image/jpeg"
	case matchAt(data, 0, "GIF87a"), matchAt(data, 0, "GIF89a"):
		return "image/gif"
	case matchAt(data, 0, "RIFF") && matchAt(data, 8, "WEBP"):
		return "image/webp"
	case matchAt(data, 0, "BM"):
		return "image/bmp"
	}
	return ""
}

// mediaTypeForPath maps a file extension to an image media type, else "".
func mediaTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return ""
}

var base64TokenRe = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

// sniffBase64Image decodes s as base64 and sniffs the result as an image.
// The whole string must be base64 (after stripping whitespace) and long
// enough that an accidental match is implausible.
func sniffBase64Image(s string) *pastedImage {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	// The magic-number sniff below is what actually confirms an image, so a
	// short threshold is safe - it only rejects trivially short strings.
	if len(compact) < 16 || !base64TokenRe.MatchString(compact) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil
	}
	if mt := sniffImageMediaType(data); mt != "" {
		if len(data) > maxAttachmentBytes {
			return nil // caller can't distinguish; size-checked again on insert
		}
		return &pastedImage{data: data, mediaType: mt}
	}
	return nil
}

// parseDataURIImage parses a "data:<media>;base64,<payload>" URI.
func parseDataURIImage(s string) *pastedImage {
	if !strings.HasPrefix(s, "data:") {
		return nil
	}
	header, payload, ok := strings.Cut(s[len("data:"):], ",")
	if !ok || !strings.HasSuffix(header, ";base64") {
		return nil
	}
	mediaType := strings.TrimSuffix(header, ";base64")
	if !strings.HasPrefix(mediaType, "image/") {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, payload))
	if err != nil {
		return nil
	}
	if sniff := sniffImageMediaType(data); sniff != "" {
		mediaType = sniff
	}
	return &pastedImage{data: data, mediaType: mediaType}
}

// readImageFile reads path (capped at maxAttachmentBytes) and returns it as
// an image, using the magic number when possible and the extension as a
// fallback. ok is false when the path isn't a readable image file.
func readImageFile(path string) (img *pastedImage, oversize, ok bool) {
	extType := mediaTypeForPath(path)
	if extType == "" {
		return nil, false, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAttachmentBytes+1))
	if err != nil {
		return nil, false, false
	}
	if len(data) > maxAttachmentBytes {
		return nil, true, false
	}
	mediaType := sniffImageMediaType(data)
	if mediaType == "" {
		mediaType = extType
	}
	return &pastedImage{data: data, mediaType: mediaType, name: filepath.Base(path)}, false, true
}

// sniffImageToken checks one whitespace-delimited paste token: a data: URI,
// a path to an image file, or base64-encoded image bytes.
func sniffImageToken(tok string) (img *pastedImage, oversize, ok bool) {
	if strings.HasPrefix(tok, "data:") {
		if img := parseDataURIImage(tok); img != nil {
			if len(img.data) > maxAttachmentBytes {
				return nil, true, false
			}
			return img, false, true
		}
		return nil, false, false
	}
	if strings.ContainsAny(tok, `/\`) || mediaTypeForPath(tok) != "" {
		if fi, err := os.Stat(tok); err == nil && fi.Mode().IsRegular() {
			return readImageFile(tok)
		}
	}
	if img := sniffBase64Image(tok); img != nil {
		return img, false, true
	}
	return nil, false, false
}

var pasteTokenRe = regexp.MustCompile(`\S+|\s+`)

// sniffWholeImage detects a paste that is a single image payload: raw bytes,
// a data: URI, or bare base64. ok is false when the whole paste isn't one.
func sniffWholeImage(content string) (img *pastedImage, oversize, ok bool) {
	if mt := sniffImageMediaType([]byte(content)); mt != "" {
		if len(content) > maxAttachmentBytes {
			return nil, true, false
		}
		return &pastedImage{data: []byte(content), mediaType: mt}, false, true
	}
	trimmed := strings.TrimSpace(content)
	if img := parseDataURIImage(trimmed); img != nil {
		if len(img.data) > maxAttachmentBytes {
			return nil, true, false
		}
		return img, false, true
	}
	if img := sniffBase64Image(trimmed); img != nil {
		return img, false, true
	}
	return nil, false, false
}

// parseImagePaste scans pasted content for embedded images. Terminals differ
// in what an image paste delivers - a temp file path (Kitty/Ghostty/WezTerm),
// a data: URI, base64 text, or the raw bytes themselves - so both the whole
// paste and each whitespace-separated token are checked. Image tokens become
// image segments (the caller replaces them with placeholders); everything
// else is kept as literal text, preserving the user's wording around inline
// references ("edit [Image #1] like this [Image #2]").
func parseImagePaste(content string) pasteParseResult {
	var res pasteParseResult
	if strings.TrimSpace(content) == "" {
		return res
	}
	// Whole-paste single image: raw bytes, data URI, or bare base64.
	if img, oversize, ok := sniffWholeImage(content); ok {
		res.segments = []pasteSegment{{image: img}}
		res.found = true
		return res
	} else if oversize {
		res.skippedOversize++
		return res
	}
	// Token-level: mixed text and image paths/URIs.
	res.segments = splitPasteTokens(content, &res)
	return res
}

// splitPasteTokens walks the paste token by token, turning image tokens into
// image segments and keeping everything else (including whitespace) as text.
func splitPasteTokens(content string, res *pasteParseResult) []pasteSegment {
	appendText := func(s string) {
		if s == "" {
			return
		}
		if n := len(res.segments); n > 0 && res.segments[n-1].image == nil {
			res.segments[n-1].text += s
			return
		}
		res.segments = append(res.segments, pasteSegment{text: s})
	}
	for _, tok := range pasteTokenRe.FindAllString(content, -1) {
		if tok[0] == ' ' || tok[0] == '\t' || tok[0] == '\n' || tok[0] == '\r' {
			appendText(tok)
			continue
		}
		img, oversize, ok := sniffImageToken(tok)
		if ok {
			res.segments = append(res.segments, pasteSegment{image: img})
			res.found = true
		} else {
			if oversize {
				res.skippedOversize++
			}
			appendText(tok)
		}
	}
	return res.segments
}

// handlePaste processes a bracketed paste (or an OSC 52 clipboard read): image
// payloads are registered as attachments and replaced inline by "[Image #N]"
// placeholder tokens at the cursor; plain text pastes insert unchanged.
func (m *model) handlePaste(content string) tea.Cmd {
	res := parseImagePaste(content)
	if !res.found {
		m.insertAtCursor(content)
		if res.skippedOversize > 0 {
			return m.setNotification(fmt.Sprintf("skipped %d image(s) over %d MiB", res.skippedOversize, maxAttachmentBytes>>20))
		}
		return nil
	}
	var sb strings.Builder
	for _, seg := range res.segments {
		if seg.image == nil {
			sb.WriteString(seg.text)
			continue
		}
		m.nextAttachmentID++
		att := inputAttachment{
			id:          m.nextAttachmentID,
			placeholder: tauchat.ImagePlaceholder(m.nextAttachmentID),
			mediaType:   seg.image.mediaType,
			data:        seg.image.data,
			name:        seg.image.name,
		}
		m.attachments = append(m.attachments, att)
		sb.WriteString(att.placeholder)
	}
	m.insertAtCursor(sb.String())
	if res.skippedOversize > 0 {
		return m.setNotification(fmt.Sprintf("skipped %d image(s) over %d MiB", res.skippedOversize, maxAttachmentBytes>>20))
	}
	return nil
}

// --- placeholder rendering --------------------------------------------------

var imagePlaceholderRe = regexp.MustCompile(`\[Image #\d+\]`)

// placeholderSpan is a [start, end) rune range into m.input covered by a live
// image placeholder token.
type placeholderSpan struct{ start, end int }

// inputSegment is a run of a wrapped input chunk, flagged when it lies inside
// a placeholder span so it can be styled differently.
type inputSegment struct {
	start, end  int
	placeholder bool
}

// imagePlaceholderSpans returns the rune ranges of m.input covered by
// placeholder tokens that have a live attachment (i.e. the attachment is
// still registered - styling is keyed on the attachment, not the bare text,
// so a user-typed "[Image #1]" with no attachment renders as normal text).
func (m *model) imagePlaceholderSpans() []placeholderSpan {
	if len(m.attachments) == 0 || m.input == "" {
		return nil
	}
	live := make(map[string]bool, len(m.attachments))
	for _, a := range m.attachments {
		live[a.placeholder] = true
	}
	var spans []placeholderSpan
	base := 0 // byte offset of s within m.input
	s := m.input
	for len(s) > 0 {
		loc := imagePlaceholderRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		if live[s[loc[0]:loc[1]]] {
			spans = append(spans, placeholderSpan{
				start: utf8.RuneCountInString(m.input[:base+loc[0]]),
				end:   utf8.RuneCountInString(m.input[:base+loc[1]]),
			})
		}
		base += loc[1]
		s = m.input[base:]
	}
	return spans
}

// splitBySpans splits the rune range [start, end) into segments, cutting at
// placeholder span boundaries so each segment is uniformly placeholder or
// plain text.
func splitBySpans(start, end int, spans []placeholderSpan) []inputSegment {
	var segs []inputSegment
	cur := start
	for _, sp := range spans {
		if sp.end <= cur || sp.start >= end {
			continue
		}
		lo := max(sp.start, cur)
		hi := min(sp.end, end)
		if lo > cur {
			segs = append(segs, inputSegment{cur, lo, false})
		}
		segs = append(segs, inputSegment{lo, hi, true})
		cur = hi
	}
	if cur < end {
		segs = append(segs, inputSegment{cur, end, false})
	}
	if len(segs) == 0 {
		segs = append(segs, inputSegment{start, end, false})
	}
	return segs
}

// renderAttachmentLines renders the "attached image: [Image #N]" rows shown
// at the bottom of the input box for live attachments.
func (m *model) renderAttachmentLines() []string {
	atts := m.liveAttachments(m.input)
	if len(atts) == 0 {
		return nil
	}
	lines := make([]string, 0, len(atts))
	for _, a := range atts {
		label := "attached image: " + a.Placeholder
		if a.Name != "" {
			label += " (" + a.Name + ")"
		}
		lines = append(lines, attachmentLineStyle.Render(" "+label))
	}
	return lines
}

// styleUserTextWithPlaceholders renders plain text (a user message line in
// scrollback) with any "[Image #N]" tokens highlighted, so recalled history
// matches the composer's styling. The base style's padding is applied once
// around the whole line, not per segment - segment-wise Render would pad
// every fragment.
func styleUserTextWithPlaceholders(s string, base lipgloss.Style) string {
	if !strings.Contains(s, "[Image #") {
		return base.Render(s)
	}
	_, _, _, left := base.GetPadding()
	inner := base.Padding(0)
	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", left))
	rest := s
	for {
		loc := imagePlaceholderRe.FindStringIndex(rest)
		if loc == nil {
			break
		}
		sb.WriteString(inner.Render(rest[:loc[0]]))
		sb.WriteString(inputAttachmentStyle.Render(rest[loc[0]:loc[1]]))
		rest = rest[loc[1]:]
	}
	sb.WriteString(inner.Render(rest))
	return sb.String()
}
