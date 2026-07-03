package api

import (
	"testing"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/stretchr/testify/require"
)

func TestProtoViewToChatNil(t *testing.T) {
	require.Nil(t, ProtoViewToChat(nil))
}

func TestProtoWidgetToChatNil(t *testing.T) {
	require.Equal(t, chat.Widget{}, ProtoWidgetToChat(nil))
}

func TestProtoWidgetToChatUnknownKindFallsBackToZeroValue(t *testing.T) {
	// A Widget with no oneof case set (e.g. a kind this host doesn't know
	// about yet) must render as nothing, not panic or default to some kind.
	got := ProtoWidgetToChat(&Widget{})
	require.Equal(t, chat.Widget{}, got)
}

func TestProtoWidgetToChatText(t *testing.T) {
	w := &Widget{Kind: &Widget_Text{Text: &TextWidget{
		Text:  "hello",
		Style: &Style{Tone: Style_TONE_SUCCESS, Bold: true},
	}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindText, got.Kind)
	require.NotNil(t, got.Text)
	require.Equal(t, "hello", got.Text.Text)
	require.NotNil(t, got.Text.Style)
	require.Equal(t, chat.ToneSuccess, got.Text.Style.Tone)
	require.True(t, got.Text.Style.Bold)
}

func TestProtoWidgetToChatStackVerticalAndHorizontal(t *testing.T) {
	vertical := ProtoWidgetToChat(&Widget{Kind: &Widget_Stack{Stack: &StackWidget{
		Direction: StackWidget_VERTICAL,
		Gap:       2,
		Children: []*Widget{
			{Kind: &Widget_Text{Text: &TextWidget{Text: "a"}}},
			{Kind: &Widget_Text{Text: &TextWidget{Text: "b"}}},
		},
	}}})
	require.Equal(t, chat.WidgetKindStack, vertical.Kind)
	require.Equal(t, chat.StackVertical, vertical.Stack.Direction)
	require.Equal(t, 2, vertical.Stack.Gap)
	require.Len(t, vertical.Stack.Children, 2)
	require.Equal(t, "a", vertical.Stack.Children[0].Text.Text)
	require.Equal(t, "b", vertical.Stack.Children[1].Text.Text)

	horizontal := ProtoWidgetToChat(&Widget{Kind: &Widget_Stack{Stack: &StackWidget{
		Direction: StackWidget_HORIZONTAL,
	}}})
	require.Equal(t, chat.StackHorizontal, horizontal.Stack.Direction)
}

func TestProtoWidgetToChatKeyValue(t *testing.T) {
	w := &Widget{Kind: &Widget_KeyValue{KeyValue: &KeyValueWidget{
		Entries: []*KeyValueWidget_Entry{
			{Key: "k1", Value: "v1"},
			{Key: "k2", Value: "v2", ValueStyle: &Style{Tone: Style_TONE_ERROR}},
		},
	}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindKeyValue, got.Kind)
	require.Len(t, got.KeyValue.Entries, 2)
	require.Equal(t, "k1", got.KeyValue.Entries[0].Key)
	require.Equal(t, "v1", got.KeyValue.Entries[0].Value)
	require.Nil(t, got.KeyValue.Entries[0].ValueStyle)
	require.NotNil(t, got.KeyValue.Entries[1].ValueStyle)
	require.Equal(t, chat.ToneError, got.KeyValue.Entries[1].ValueStyle.Tone)
}

func TestProtoWidgetToChatList(t *testing.T) {
	w := &Widget{Kind: &Widget_List{List: &ListWidget{
		Items:   []string{"one", "two"},
		Ordered: true,
	}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindList, got.Kind)
	require.Equal(t, []string{"one", "two"}, got.List.Items)
	require.True(t, got.List.Ordered)
}

func TestProtoWidgetToChatTable(t *testing.T) {
	w := &Widget{Kind: &Widget_Table{Table: &TableWidget{
		Headers: []string{"name", "status"},
		Rows: []*TableWidget_Row{
			{Cells: []string{"go", "ok"}},
			{Cells: []string{"lint", "failed"}},
		},
	}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindTable, got.Kind)
	require.Equal(t, []string{"name", "status"}, got.Table.Headers)
	require.Len(t, got.Table.Rows, 2)
	require.Equal(t, []string{"go", "ok"}, got.Table.Rows[0].Cells)
}

func TestProtoWidgetToChatProgress(t *testing.T) {
	w := &Widget{Kind: &Widget_Progress{Progress: &ProgressWidget{
		Label:    "loading",
		Fraction: 0.5,
	}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindProgress, got.Kind)
	require.Equal(t, "loading", got.Progress.Label)
	require.InDelta(t, 0.5, got.Progress.Fraction, 0.0001)
}

func TestProtoWidgetToChatDivider(t *testing.T) {
	w := &Widget{Kind: &Widget_Divider{Divider: &DividerWidget{Label: "Results"}}}
	got := ProtoWidgetToChat(w)
	require.Equal(t, chat.WidgetKindDivider, got.Kind)
	require.Equal(t, "Results", got.Divider.Label)
}

func TestProtoWidgetToChatStatusStates(t *testing.T) {
	cases := []struct {
		proto StatusWidget_State
		want  chat.StatusState
	}{
		{StatusWidget_RUNNING, chat.StatusRunning},
		{StatusWidget_SUCCESS, chat.StatusSuccess},
		{StatusWidget_FAILED, chat.StatusFailed},
		{StatusWidget_NEUTRAL, chat.StatusNeutral},
	}
	for _, tc := range cases {
		w := &Widget{Kind: &Widget_Status{Status: &StatusWidget{
			State:  tc.proto,
			Label:  "label",
			Detail: "detail",
		}}}
		got := ProtoWidgetToChat(w)
		require.Equal(t, chat.WidgetKindStatus, got.Kind)
		require.Equal(t, tc.want, got.Status.State, "proto state %v", tc.proto)
		require.Equal(t, "label", got.Status.Label)
		require.Equal(t, "detail", got.Status.Detail)
	}
}

func TestProtoViewToChatFull(t *testing.T) {
	v := &View{
		Id:    "panel-1",
		Title: "My Panel",
		Style: &Style{Tone: Style_TONE_INFO},
		Widgets: []*Widget{
			{Kind: &Widget_Text{Text: &TextWidget{Text: "hi"}}},
			{Kind: &Widget_Divider{Divider: &DividerWidget{}}},
		},
	}
	got := ProtoViewToChat(v)
	require.NotNil(t, got)
	require.Equal(t, "panel-1", got.ID)
	require.Equal(t, "My Panel", got.Title)
	require.NotNil(t, got.Style)
	require.Equal(t, chat.ToneInfo, got.Style.Tone)
	require.Len(t, got.Widgets, 2)
	require.Equal(t, chat.WidgetKindText, got.Widgets[0].Kind)
	require.Equal(t, chat.WidgetKindDivider, got.Widgets[1].Kind)
}

func TestProtoStyleToChatAllTonesAndHexFields(t *testing.T) {
	cases := []struct {
		proto Style_Tone
		want  chat.StyleTone
	}{
		{Style_TONE_DEFAULT, chat.ToneDefault},
		{Style_TONE_INFO, chat.ToneInfo},
		{Style_TONE_SUCCESS, chat.ToneSuccess},
		{Style_TONE_WARN, chat.ToneWarn},
		{Style_TONE_ERROR, chat.ToneError},
		{Style_TONE_MUTED, chat.ToneMuted},
	}
	for _, tc := range cases {
		w := ProtoWidgetToChat(&Widget{Kind: &Widget_Text{Text: &TextWidget{
			Text:  "x",
			Style: &Style{Tone: tc.proto},
		}}})
		require.Equal(t, tc.want, w.Text.Style.Tone, "proto tone %v", tc.proto)
	}

	styled := ProtoWidgetToChat(&Widget{Kind: &Widget_Text{Text: &TextWidget{
		Text: "x",
		Style: &Style{
			FgHex: "#ffae00", BgHex: "#000000",
			Bold: true, Dim: true, Italic: true, Underline: true,
		},
	}}})
	require.Equal(t, "#ffae00", styled.Text.Style.FgHex)
	require.Equal(t, "#000000", styled.Text.Style.BgHex)
	require.True(t, styled.Text.Style.Bold)
	require.True(t, styled.Text.Style.Dim)
	require.True(t, styled.Text.Style.Italic)
	require.True(t, styled.Text.Style.Underline)
}
