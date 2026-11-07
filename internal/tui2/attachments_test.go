package tui2

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), []byte("fake-png-payload")...)

func TestParseImagePastePlainText(t *testing.T) {
	res := parseImagePaste("just some pasted text")
	if res.found {
		t.Fatalf("found = true for plain text: %+v", res.segments)
	}
}

func TestParseImagePasteRawBytes(t *testing.T) {
	res := parseImagePaste(string(testPNG))
	if !res.found || len(res.segments) != 1 || res.segments[0].image == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.segments[0].image.mediaType != "image/png" {
		t.Fatalf("mediaType = %q", res.segments[0].image.mediaType)
	}
	if string(res.segments[0].image.data) != string(testPNG) {
		t.Fatalf("data mismatch")
	}
}

func TestParseImagePasteBase64(t *testing.T) {
	res := parseImagePaste(base64.StdEncoding.EncodeToString(testPNG))
	if !res.found || len(res.segments) != 1 || res.segments[0].image == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.segments[0].image.mediaType != "image/png" || string(res.segments[0].image.data) != string(testPNG) {
		t.Fatalf("image = %+v", res.segments[0].image)
	}
}

func TestParseImagePasteDataURI(t *testing.T) {
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG)
	res := parseImagePaste(uri)
	if !res.found || len(res.segments) != 1 || res.segments[0].image == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.segments[0].image.mediaType != "image/png" {
		t.Fatalf("mediaType = %q", res.segments[0].image.mediaType)
	}
}

func TestParseImagePasteFilePath(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "screenshot.png")
	if err := os.WriteFile(pngPath, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Single image path.
	res := parseImagePaste(pngPath)
	if !res.found || len(res.segments) != 1 || res.segments[0].image == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.segments[0].image.mediaType != "image/png" || res.segments[0].image.name != "screenshot.png" {
		t.Fatalf("image = %+v", res.segments[0].image)
	}

	// Mixed text and image path: text is preserved around the image segment.
	res = parseImagePaste("look at " + pngPath + " please")
	if !res.found {
		t.Fatal("found = false")
	}
	var texts, images int
	for _, seg := range res.segments {
		if seg.image != nil {
			images++
		} else {
			texts++
		}
	}
	if images != 1 || texts == 0 {
		t.Fatalf("segments = %+v", res.segments)
	}
	var joined strings.Builder
	for _, seg := range res.segments {
		if seg.image != nil {
			joined.WriteString("<IMG>")
		} else {
			joined.WriteString(seg.text)
		}
	}
	if joined.String() != "look at <IMG> please" {
		t.Fatalf("joined = %q", joined.String())
	}

	// Non-image file is not detected.
	if res := parseImagePaste(txtPath); res.found {
		t.Fatalf("txt path detected as image: %+v", res.segments)
	}
}

func TestParseImagePasteMultipleImagePaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}

	res := parseImagePaste(a + " " + b)
	if !res.found || len(res.segments) != 3 {
		t.Fatalf("segments = %+v", res.segments)
	}
	if res.segments[0].image == nil || res.segments[1].image != nil || res.segments[2].image == nil {
		t.Fatalf("segments = %+v", res.segments)
	}
}

func TestHandlePasteImageInsertsPlaceholder(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "describe "
	m.inputCursor = len([]rune(m.input))

	cmd := m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	if cmd != nil {
		t.Fatalf("unexpected cmd: %v", cmd)
	}
	if m.input != "describe [Image #1]" {
		t.Fatalf("input = %q", m.input)
	}
	if len(m.attachments) != 1 {
		t.Fatalf("attachments = %+v", m.attachments)
	}
	att := m.attachments[0]
	if att.placeholder != "[Image #1]" || att.mediaType != "image/png" || string(att.data) != string(testPNG) {
		t.Fatalf("attachment = %+v", att)
	}
}

func TestHandlePastePlainTextUnchanged(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handlePaste("pasted text")
	if m.input != "pasted text" || len(m.attachments) != 0 {
		t.Fatalf("input = %q, attachments = %+v", m.input, m.attachments)
	}
}

func TestHandlePasteMultipleImagesGetDistinctPlaceholders(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, testPNG, 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(&fakeRuntime{}, nil)
	m.handlePaste(a + " " + b)
	if m.input != "[Image #1] [Image #2]" {
		t.Fatalf("input = %q", m.input)
	}
	if len(m.attachments) != 2 || m.attachments[1].placeholder != "[Image #2]" {
		t.Fatalf("attachments = %+v", m.attachments)
	}
}

func TestPasteMsgRoutesThroughHandlePaste(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	_, cmd := m.Update(tea.PasteMsg{Content: base64.StdEncoding.EncodeToString(testPNG)})
	if cmd != nil {
		t.Fatalf("unexpected cmd")
	}
	if m.input != "[Image #1]" || len(m.attachments) != 1 {
		t.Fatalf("input = %q, attachments = %+v", m.input, m.attachments)
	}
}

func TestLiveAttachmentsDropsDeletedPlaceholder(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "describe [Image #1]"

	atts := m.liveAttachments(m.input)
	if len(atts) != 1 || atts[0].Placeholder != "[Image #1]" {
		t.Fatalf("atts = %+v", atts)
	}

	// User deletes the placeholder: the attachment is no longer live.
	m.input = "describe"
	if atts := m.liveAttachments(m.input); len(atts) != 0 {
		t.Fatalf("atts after deleting placeholder = %+v", atts)
	}
}

func TestSubmitSendsAttachmentsWithPrompt(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "what is in " + m.input
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.submitInput())

	if len(rt.sent) != 1 {
		t.Fatalf("sent = %d", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("sent = %T", rt.sent[0])
	}
	if cmd.Prompt != "what is in [Image #1]" {
		t.Fatalf("prompt = %q", cmd.Prompt)
	}
	if len(cmd.Attachments) != 1 || cmd.Attachments[0].Placeholder != "[Image #1]" || cmd.Attachments[0].MediaType != "image/png" {
		t.Fatalf("attachments = %+v", cmd.Attachments)
	}
	if string(cmd.Attachments[0].Data) != string(testPNG) {
		t.Fatalf("attachment data mismatch")
	}
	// Composer state is reset after submit.
	if m.input != "" || len(m.attachments) != 0 {
		t.Fatalf("input = %q, attachments = %+v after submit", m.input, m.attachments)
	}
}

func TestQueuedTurnKeepsAttachments(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.agentState = agentThinking // response in flight -> queues
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "queued " + m.input

	drainCmd(m.submitInput())

	if len(m.turnQueue) != 1 {
		t.Fatalf("turnQueue = %+v", m.turnQueue)
	}
	if m.turnQueue[0].text != "queued [Image #1]" {
		t.Fatalf("queued text = %q", m.turnQueue[0].text)
	}
	if len(m.turnQueue[0].attachments) != 1 || m.turnQueue[0].attachments[0].Placeholder != "[Image #1]" {
		t.Fatalf("queued attachments = %+v", m.turnQueue[0].attachments)
	}
}

func TestRenderInputAreaShowsStyledPlaceholderAndAttachmentLines(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "describe " + m.input
	m.inputCursor = len([]rune(m.input))

	out := m.renderInputArea()
	plain := stripANSI(out)
	if !strings.Contains(plain, "describe [Image #1]") {
		t.Fatalf("input area missing placeholder text:\n%s", plain)
	}
	if !strings.Contains(plain, "attached image: [Image #1]") {
		t.Fatalf("input area missing attachment line:\n%s", plain)
	}
	// The inline placeholder is styled differently from the surrounding text.
	// The cursor path renders per-rune, so match the attachment color code
	// opening the placeholder's first rune rather than one contiguous run.
	if !strings.Contains(out, "\x1b[1;38;2;120;170;255m[") && !strings.Contains(out, inputAttachmentStyle.Render("[Image #1]")) {
		t.Fatalf("placeholder not rendered with inputAttachmentStyle:\n%q", out)
	}
	// The attachment row uses the green attachment line style.
	if !strings.Contains(out, attachmentLineStyle.Render(" attached image: [Image #1]")) {
		t.Fatalf("attachment line not rendered with attachmentLineStyle:\n%q", out)
	}
}

func TestRenderInputAreaHidesAttachmentLineWhenPlaceholderDeleted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 80
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "no reference anymore"
	m.inputCursor = len([]rune(m.input))

	plain := stripANSI(m.renderInputArea())
	if strings.Contains(plain, "attached image:") {
		t.Fatalf("attachment line should be hidden once the placeholder is gone:\n%s", plain)
	}
}

func TestImagePlaceholderSpansAndSplit(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.attachments = []inputAttachment{
		{id: 1, placeholder: "[Image #1]"},
		{id: 2, placeholder: "[Image #2]"},
	}
	m.input = "a [Image #1] b [Image #2] c [Image #9]"
	spans := m.imagePlaceholderSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %+v", spans)
	}
	rs := []rune(m.input)
	if string(rs[spans[0].start:spans[0].end]) != "[Image #1]" || string(rs[spans[1].start:spans[1].end]) != "[Image #2]" {
		t.Fatalf("span text = %q, %q", string(rs[spans[0].start:spans[0].end]), string(rs[spans[1].start:spans[1].end]))
	}

	segs := splitBySpans(0, len(rs), spans)
	var got []string
	for _, seg := range segs {
		got = append(got, string(rs[seg.start:seg.end]))
	}
	want := []string{"a ", "[Image #1]", " b ", "[Image #2]", " c [Image #9]"}
	if len(got) != len(want) {
		t.Fatalf("segs = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segs = %q, want %q", got, want)
		}
	}
}

func TestStyleUserTextWithPlaceholders(t *testing.T) {
	out := styleUserTextWithPlaceholders("look at [Image #1] now", userStyle)
	if !strings.Contains(out, inputAttachmentStyle.Render("[Image #1]")) {
		t.Fatalf("placeholder not styled: %q", out)
	}
	plain := stripANSI(out)
	// Base style padding is applied exactly once around the whole line.
	if plain != strings.Repeat(" ", userStyle.GetPaddingLeft())+"look at [Image #1] now" {
		t.Fatalf("plain = %q", plain)
	}
}

func testModelWithInputs(inputs ...string) []tauchat.ChatModelRef {
	return []tauchat.ChatModelRef{{
		ID:       "gpt",
		Provider: "openai",
		Config:   tauconfig.ModelConfig{ID: "gpt", Input: inputs},
	}}
}

func TestImageGateBlocksTextOnlyModel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = testModelWithInputs("text")
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "describe " + m.input
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.submitInput())

	if !strings.Contains(m.notification, "doesn't accept images") {
		t.Fatalf("notification = %q", m.notification)
	}
	// Composer stays intact and nothing was sent.
	if m.input != "describe [Image #1]" {
		t.Fatalf("input = %q, want preserved", m.input)
	}
	if len(m.attachments) != 1 {
		t.Fatalf("attachments = %+v, want preserved", m.attachments)
	}
	if len(rt.sent) != 0 {
		t.Fatalf("sent = %d, want 0 (blocked)", len(rt.sent))
	}
}

func TestImageGateAllowsVisionModel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = testModelWithInputs("text", "image")
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "describe " + m.input
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.submitInput())

	if len(rt.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(rt.sent))
	}
	cmd, ok := rt.sent[0].(tauchat.SubmitChatPromptCommand)
	if !ok {
		t.Fatalf("sent = %T", rt.sent[0])
	}
	if len(cmd.Attachments) != 1 {
		t.Fatalf("attachments = %+v", cmd.Attachments)
	}
}

func TestImageGateFailsOpenWhenModelUnknown(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	// No availableModels entry for the current model (live-discovered models
	// carry no modality data): the gate must fail open, not block.
	m.availableModels = nil
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "describe " + m.input
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.submitInput())

	if len(rt.sent) != 1 {
		t.Fatalf("sent = %d, want 1 (fail-open)", len(rt.sent))
	}
}

func TestSteerAlsoGatedForTextOnlyModel(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = testModelWithInputs("text")
	m.agentState = agentThinking // in-flight -> steer path
	m.handlePaste(base64.StdEncoding.EncodeToString(testPNG))
	m.input = "steer " + m.input
	m.inputCursor = len([]rune(m.input))

	drainCmd(m.handleSteer())

	if !strings.Contains(m.notification, "doesn't accept images") {
		t.Fatalf("notification = %q", m.notification)
	}
	if len(rt.sent) != 0 {
		t.Fatalf("sent = %d, want 0 (blocked)", len(rt.sent))
	}
	if m.input != "steer [Image #1]" {
		t.Fatalf("input = %q, want preserved", m.input)
	}
}
