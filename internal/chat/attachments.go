package chat

import (
	"regexp"
	"strconv"
	"strings"
)

// ChatAttachment is a binary payload (typically an image) attached to a
// user prompt. The message content references it inline via Placeholder
// (e.g. "[Image #1]"), in the same style as image-editing model prompts
// where images are referenced at the exact position they apply to.
type ChatAttachment struct {
	// Placeholder is the inline reference token in the message content
	// (e.g. "[Image #1]"). It is how the model (and the UI) tie the text
	// position to this payload.
	Placeholder string `json:"placeholder"`
	// MediaType is the IANA media type of Data (e.g. "image/png").
	MediaType string `json:"media_type"`
	// Data holds the raw payload bytes. encoding/json marshals []byte as
	// base64, so attachments round-trip through JSON (store, wire
	// protocol) without extra encoding.
	Data []byte `json:"data"`
	// Name is an optional original file name, for display only.
	Name string `json:"name,omitempty"`
}

// IsImage reports whether the attachment is an image media type.
func (a ChatAttachment) IsImage() bool {
	return strings.HasPrefix(a.MediaType, "image/")
}

// imagePlaceholderPattern matches inline image references like "[Image #1]".
var imagePlaceholderPattern = regexp.MustCompile(`\[Image #(\d+)\]`)

// ImagePlaceholder formats the inline reference token for the n-th image
// (1-based, matching what users see in the composer).
func ImagePlaceholder(n int) string {
	return "[Image #" + strconv.Itoa(n) + "]"
}

// FindImagePlaceholder returns the byte offsets [start, end) of the first
// placeholder token in s, or (-1, -1) when s contains none.
func FindImagePlaceholder(s string) (start, end int) {
	loc := imagePlaceholderPattern.FindStringIndex(s)
	if loc == nil {
		return -1, -1
	}
	return loc[0], loc[1]
}

// ImagePlaceholderSpans returns the byte offsets [start, end) of every
// placeholder token in s, in order.
func ImagePlaceholderSpans(s string) [][2]int {
	locs := imagePlaceholderPattern.FindAllStringIndex(s, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// ImagePlaceholderRefs returns the placeholder tokens present in text, in
// order of first appearance, deduplicated. Unparseable or zero-numbered
// tokens are ignored.
func ImagePlaceholderRefs(text string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range imagePlaceholderPattern.FindAllStringSubmatch(text, -1) {
		if m[1] == "0" {
			continue
		}
		tok := m[0]
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out
}

// ResolveImageAttachments returns the subset of attachments whose
// placeholder actually appears in text, ordered by first appearance of the
// placeholder in text (not by attachment slice order). Attachments without
// a placeholder, without data, or whose placeholder is absent are dropped -
// deleting the placeholder from the composer detaches the image.
func ResolveImageAttachments(text string, attachments []ChatAttachment) []ChatAttachment {
	refs := ImagePlaceholderRefs(text)
	if len(refs) == 0 {
		return nil
	}
	byPlaceholder := make(map[string]ChatAttachment, len(attachments))
	for _, a := range attachments {
		if a.Placeholder == "" || len(a.Data) == 0 || a.MediaType == "" {
			continue
		}
		if _, ok := byPlaceholder[a.Placeholder]; !ok {
			byPlaceholder[a.Placeholder] = a
		}
	}
	out := make([]ChatAttachment, 0, len(refs))
	for _, ref := range refs {
		if a, ok := byPlaceholder[ref]; ok {
			out = append(out, a)
		}
	}
	return out
}
