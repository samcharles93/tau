package api

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
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
		Direction: StackWidget_DIRECTION_VERTICAL,
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
		Direction: StackWidget_DIRECTION_HORIZONTAL,
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
		{StatusWidget_STATE_RUNNING, chat.StatusRunning},
		{StatusWidget_STATE_SUCCESS, chat.StatusSuccess},
		{StatusWidget_STATE_FAILED, chat.StatusFailed},
		{StatusWidget_STATE_NEUTRAL, chat.StatusNeutral},
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

// -- stubs ----------------------------------------------------------------

// stubExtension implements Extension with configurable returns. It always
// satisfies HostAware, Capable, and Documented so individual tests can set
// the fields that matter.
type stubExtension struct {
	name     string
	commands []*Command
	metadata func() (string, []*Command)

	runCommandOut  string
	runCommandView *View
	runCommandErr  error
	runCommandName string
	runCommandArgs string

	reloadDiags    []*Diagnostic
	reloadCommands []*Command
	reloadErr      error

	toolsOut []*ToolDefinition
	toolsErr error

	executeToolContent string
	executeToolIsError bool
	executeToolErr     error
	executeToolName    string
	executeToolArgs    string

	dispatchEventResp      *EventResponse
	dispatchEventName      string
	dispatchEventSessionID string
	dispatchEventPayload   *EventPayload

	host         Host
	capabilities []string
	docs         string
}

func (s *stubExtension) Metadata() (string, []*Command) {
	if s.metadata != nil {
		return s.metadata()
	}
	return s.name, s.commands
}

func (s *stubExtension) RunCommand(_ context.Context, name, args string) (string, *View, error) {
	s.runCommandName = name
	s.runCommandArgs = args
	return s.runCommandOut, s.runCommandView, s.runCommandErr
}

func (s *stubExtension) Reload(_ context.Context) ([]*Diagnostic, []*Command, error) {
	return s.reloadDiags, s.reloadCommands, s.reloadErr
}

func (s *stubExtension) Tools(_ context.Context) ([]*ToolDefinition, error) {
	return s.toolsOut, s.toolsErr
}

func (s *stubExtension) ExecuteTool(_ context.Context, name, args string) (string, bool, error) {
	s.executeToolName = name
	s.executeToolArgs = args
	return s.executeToolContent, s.executeToolIsError, s.executeToolErr
}

func (s *stubExtension) DispatchEvent(_ context.Context, event, sessionID string, payload *EventPayload) *EventResponse {
	s.dispatchEventName = event
	s.dispatchEventSessionID = sessionID
	s.dispatchEventPayload = payload
	return s.dispatchEventResp
}

func (s *stubExtension) SetHost(h Host)         { s.host = h }
func (s *stubExtension) Capabilities() []string { return s.capabilities }
func (s *stubExtension) Docs() string           { return s.docs }

// extensionOnly wraps an Extension and hides optional interfaces.
type extensionOnly struct{ Extension }

// stubExtensionServiceClient implements ExtensionServiceClient with
// configurable per-method responses.
type stubExtensionServiceClient struct {
	getMetadataResp *GetMetadataResponse
	getMetadataErr  error
	runCommandResp  *RunCommandResponse
	runCommandErr   error
	reloadResp      *ReloadResponse
	reloadErr       error
	dispatchResp    *DispatchEventResponse
	dispatchErr     error
	getToolsResp    *GetToolsResponse
	getToolsErr     error
	executeToolResp *ExecuteToolResponse
	executeToolErr  error
	logResp         *LogResponse
	logErr          error
	logReq          *LogRequest
	initErr         error
	getCapsResp     *GetCapabilitiesResponse
	getCapsErr      error
}

func (c *stubExtensionServiceClient) GetMetadata(_ context.Context, _ *GetMetadataRequest, _ ...grpc.CallOption) (*GetMetadataResponse, error) {
	return c.getMetadataResp, c.getMetadataErr
}

func (c *stubExtensionServiceClient) RunCommand(_ context.Context, _ *RunCommandRequest, _ ...grpc.CallOption) (*RunCommandResponse, error) {
	return c.runCommandResp, c.runCommandErr
}

func (c *stubExtensionServiceClient) Reload(_ context.Context, _ *ReloadRequest, _ ...grpc.CallOption) (*ReloadResponse, error) {
	return c.reloadResp, c.reloadErr
}

func (c *stubExtensionServiceClient) DispatchEvent(_ context.Context, _ *DispatchEventRequest, _ ...grpc.CallOption) (*DispatchEventResponse, error) {
	return c.dispatchResp, c.dispatchErr
}

func (c *stubExtensionServiceClient) GetTools(_ context.Context, _ *GetToolsRequest, _ ...grpc.CallOption) (*GetToolsResponse, error) {
	return c.getToolsResp, c.getToolsErr
}

func (c *stubExtensionServiceClient) ExecuteTool(_ context.Context, _ *ExecuteToolRequest, _ ...grpc.CallOption) (*ExecuteToolResponse, error) {
	return c.executeToolResp, c.executeToolErr
}

func (c *stubExtensionServiceClient) Log(_ context.Context, req *LogRequest, _ ...grpc.CallOption) (*LogResponse, error) {
	c.logReq = req
	return c.logResp, c.logErr
}

func (c *stubExtensionServiceClient) Init(_ context.Context, _ *InitRequest, _ ...grpc.CallOption) (*InitResponse, error) {
	return &InitResponse{}, c.initErr
}

func (c *stubExtensionServiceClient) GetCapabilities(_ context.Context, _ *GetCapabilitiesRequest, _ ...grpc.CallOption) (*GetCapabilitiesResponse, error) {
	return c.getCapsResp, c.getCapsErr
}

// stubHostServiceClient implements HostServiceClient.
type stubHostServiceClient struct {
	getConfigResp       *GetConfigResponse
	getConfigErr        error
	getConfigReq        *GetConfigRequest
	setConfigErr        error
	getSessionStateResp *GetSessionStateResponse
	getSessionStateErr  error
	getModelsResp       *GetAvailableModelsResponse
	getModelsErr        error
	notifyErr           error
	confirmResp         *ConfirmResponse
	confirmErr          error
	inputResp           *InputResponse
	inputErr            error
	logReq              *LogRequest
	logErr              error
	renderViewErr       error
	closeViewErr        error
}

func (c *stubHostServiceClient) GetConfig(_ context.Context, req *GetConfigRequest, _ ...grpc.CallOption) (*GetConfigResponse, error) {
	c.getConfigReq = req
	return c.getConfigResp, c.getConfigErr
}

func (c *stubHostServiceClient) SetConfig(_ context.Context, _ *SetConfigRequest, _ ...grpc.CallOption) (*SetConfigResponse, error) {
	return &SetConfigResponse{}, c.setConfigErr
}

func (c *stubHostServiceClient) GetSessionState(_ context.Context, _ *GetSessionStateRequest, _ ...grpc.CallOption) (*GetSessionStateResponse, error) {
	return c.getSessionStateResp, c.getSessionStateErr
}

func (c *stubHostServiceClient) GetAvailableModels(_ context.Context, _ *GetAvailableModelsRequest, _ ...grpc.CallOption) (*GetAvailableModelsResponse, error) {
	return c.getModelsResp, c.getModelsErr
}

func (c *stubHostServiceClient) Notify(_ context.Context, _ *NotifyRequest, _ ...grpc.CallOption) (*NotifyResponse, error) {
	return &NotifyResponse{}, c.notifyErr
}

func (c *stubHostServiceClient) Log(_ context.Context, req *LogRequest, _ ...grpc.CallOption) (*LogResponse, error) {
	c.logReq = req
	return &LogResponse{}, c.logErr
}

func (c *stubHostServiceClient) Confirm(_ context.Context, _ *ConfirmRequest, _ ...grpc.CallOption) (*ConfirmResponse, error) {
	return c.confirmResp, c.confirmErr
}

func (c *stubHostServiceClient) Input(_ context.Context, _ *InputRequest, _ ...grpc.CallOption) (*InputResponse, error) {
	return c.inputResp, c.inputErr
}

func (c *stubHostServiceClient) RenderView(_ context.Context, _ *RenderViewRequest, _ ...grpc.CallOption) (*RenderViewResponse, error) {
	return &RenderViewResponse{}, c.renderViewErr
}

func (c *stubHostServiceClient) CloseView(_ context.Context, _ *CloseViewRequest, _ ...grpc.CallOption) (*CloseViewResponse, error) {
	return &CloseViewResponse{}, c.closeViewErr
}

// -- 1. protoCommandsToChat + protoDiagnosticsToChat ---------------------

func TestProtoCommandsToChat(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		// make([]T, 0) produces empty (non-nil) slice when len(nil)==0.
		require.Equal(t, []chat.ExtensionCommand{}, protoCommandsToChat(nil))
	})
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, []chat.ExtensionCommand{}, protoCommandsToChat([]*Command{}))
	})
	t.Run("flat commands", func(t *testing.T) {
		cmds := []*Command{
			{Name: "run", Description: "do the thing", ExtensionName: "p1", ArgsHint: "<file>"},
			{Name: "lint"},
		}
		got := protoCommandsToChat(cmds)
		require.Len(t, got, 2)
		require.Equal(t, "run", got[0].Name)
		require.Equal(t, "do the thing", got[0].Description)
		require.Equal(t, "p1", got[0].ExtensionName)
		require.Equal(t, "<file>", got[0].ArgsHint)
		require.Empty(t, got[0].Subcommands)
		require.Equal(t, "lint", got[1].Name)
	})
	t.Run("nested subcommands", func(t *testing.T) {
		cmds := []*Command{{
			Name: "mcp",
			Subcommands: []*Command{
				{Name: "list"},
				{Name: "reconnect", ArgsHint: "<server>"},
			},
		}}
		got := protoCommandsToChat(cmds)
		require.Len(t, got, 1)
		require.Len(t, got[0].Subcommands, 2)
		require.Equal(t, "list", got[0].Subcommands[0].Name)
		require.Equal(t, "reconnect", got[0].Subcommands[1].Name)
		require.Equal(t, "<server>", got[0].Subcommands[1].ArgsHint)
	})
}

func TestProtoDiagnosticsToChat(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		require.Equal(t, []chat.ExtensionDiagnostic{}, protoDiagnosticsToChat(nil))
	})
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, []chat.ExtensionDiagnostic{}, protoDiagnosticsToChat([]*Diagnostic{}))
	})
	t.Run("multiple entries", func(t *testing.T) {
		diags := []*Diagnostic{
			{Path: "a", ExtensionName: "x", Severity: "error", Message: "boom"},
			{Path: "b", ExtensionName: "y", Severity: "warn", Message: "hm"},
		}
		got := protoDiagnosticsToChat(diags)
		require.Len(t, got, 2)
		require.Equal(t, chat.ExtensionDiagnostic{Path: "a", ExtensionName: "x", Severity: "error", Message: "boom"}, got[0])
		require.Equal(t, chat.ExtensionDiagnostic{Path: "b", ExtensionName: "y", Severity: "warn", Message: "hm"}, got[1])
	})
}

// -- 2. GRPCServer.GetCapabilities (defaults vs explicit) -----------------

func TestGRPCServerGetCapabilities(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit capabilities from Capable extension", func(t *testing.T) {
		ext := &stubExtension{capabilities: []string{"commands", "views"}}
		srv := &GRPCServer{Impl: ext}
		resp, err := srv.GetCapabilities(ctx, &GetCapabilitiesRequest{})
		require.NoError(t, err)
		require.Equal(t, []string{"commands", "views"}, resp.GetCapabilities())
	})

	t.Run("defaults when extension is not Capable", func(t *testing.T) {
		ext := extensionOnly{Extension: &stubExtension{}}
		srv := &GRPCServer{Impl: ext}
		resp, err := srv.GetCapabilities(ctx, &GetCapabilitiesRequest{})
		require.NoError(t, err)
		require.Equal(t, []string{CapabilityCommands, CapabilityTools, CapabilityEvents}, resp.GetCapabilities())
	})
}

// -- 3. GRPCServer.GetMetadata (Documented optional interface) ------------

func TestGRPCServerGetMetadata(t *testing.T) {
	ctx := context.Background()

	t.Run("without Documented", func(t *testing.T) {
		ext := extensionOnly{Extension: &stubExtension{name: "p1", commands: []*Command{{Name: "x"}}}}
		srv := &GRPCServer{Impl: ext}
		resp, err := srv.GetMetadata(ctx, &GetMetadataRequest{})
		require.NoError(t, err)
		require.Equal(t, "p1", resp.GetName())
		require.Len(t, resp.GetCommands(), 1)
		require.Equal(t, "", resp.GetDocs())
	})

	t.Run("with Documented", func(t *testing.T) {
		ext := &stubExtension{name: "p2", docs: "# Plugin docs"}
		srv := &GRPCServer{Impl: ext}
		resp, err := srv.GetMetadata(ctx, &GetMetadataRequest{})
		require.NoError(t, err)
		require.Equal(t, "p2", resp.GetName())
		require.Equal(t, "# Plugin docs", resp.GetDocs())
	})
}

// -- 4. GRPCClient.ReloadExtensions (full conversion path) ---------------

func TestGRPCClientReloadExtensions(t *testing.T) {
	ctx := context.Background()

	t.Run("converts commands and diagnostics", func(t *testing.T) {
		stub := &stubExtensionServiceClient{
			reloadResp: &ReloadResponse{
				Commands:    []*Command{{Name: "r"}},
				Diagnostics: []*Diagnostic{{Path: "f", Severity: "warn", Message: "m"}},
			},
		}
		c := &GRPCClient{Client: stub}
		res, err := c.ReloadExtensions(ctx, false)
		require.NoError(t, err)
		require.Equal(t, 1, res.ExtensionCount)
		require.Len(t, res.Commands, 1)
		require.Equal(t, "r", res.Commands[0].Name)
		require.Len(t, res.Diagnostics, 1)
		require.Equal(t, "f", res.Diagnostics[0].Path)
	})

	t.Run("RPC error propagated", func(t *testing.T) {
		stub := &stubExtensionServiceClient{reloadErr: errors.New("gone")}
		c := &GRPCClient{Client: stub}
		_, err := c.ReloadExtensions(ctx, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "gone")
	})
}

// -- 5. GRPCClient.ExtensionCommands (caching after Reload) --------------

func TestGRPCClientExtensionCommandsCaching(t *testing.T) {
	// After ReloadExtensions populates cmds, ExtensionCommands() must return
	// cached commands without making another GetMetadata RPC.
	stub := &stubExtensionServiceClient{
		reloadResp:     &ReloadResponse{Commands: []*Command{{Name: "cached"}}},
		getMetadataErr: errors.New("should not be called"),
	}
	c := &GRPCClient{Client: stub}

	_, err := c.ReloadExtensions(context.Background(), false)
	require.NoError(t, err)

	cmds := c.ExtensionCommands()
	require.Len(t, cmds, 1)
	require.Equal(t, "cached", cmds[0].Name)
}

// -- 6. GRPCClient.Docs (separate docsFetched cache) ---------------------

func TestGRPCClientDocsCaching(t *testing.T) {
	ctx := context.Background()

	t.Run("fetches and caches", func(t *testing.T) {
		stub := &stubExtensionServiceClient{
			getMetadataResp: &GetMetadataResponse{Docs: "# Doc", Commands: []*Command{{Name: "x"}}},
		}
		c := &GRPCClient{Client: stub}

		require.Equal(t, "# Doc", c.Docs(ctx))
		// Second call hits cache.
		stub.getMetadataResp = nil
		stub.getMetadataErr = errors.New("unreachable")
		require.Equal(t, "# Doc", c.Docs(ctx))
	})

	t.Run("error returns empty string", func(t *testing.T) {
		stub := &stubExtensionServiceClient{getMetadataErr: errors.New("gone")}
		c := &GRPCClient{Client: stub}
		require.Equal(t, "", c.Docs(ctx))
	})
}

// -- 7. hostClient.Confirm / Input (ErrPromptCanceled) -------------------

func TestHostClientConfirmCanceled(t *testing.T) {
	ctx := context.Background()

	t.Run("canceled returns false + ErrPromptCanceled", func(t *testing.T) {
		h := &hostClient{
			client: &stubHostServiceClient{
				confirmResp: &ConfirmResponse{Canceled: true},
			},
		}
		ok, err := h.Confirm(ctx, "title", "desc")
		require.False(t, ok)
		require.ErrorIs(t, err, ErrPromptCanceled)
	})

	t.Run("confirmed returns true", func(t *testing.T) {
		h := &hostClient{
			client: &stubHostServiceClient{
				confirmResp: &ConfirmResponse{Confirmed: true},
			},
		}
		ok, err := h.Confirm(ctx, "title", "desc")
		require.True(t, ok)
		require.NoError(t, err)
	})
}

func TestHostClientInputCanceled(t *testing.T) {
	ctx := context.Background()

	h := &hostClient{
		client: &stubHostServiceClient{
			inputResp: &InputResponse{Canceled: true},
		},
	}
	val, err := h.Input(ctx, "title", "hint")
	require.Equal(t, "", val)
	require.ErrorIs(t, err, ErrPromptCanceled)
}

// -- 8. hostClient.GetConfig (plugin name scoping) -----------------------

func TestHostClientGetConfigScopesPluginName(t *testing.T) {
	ctx := context.Background()

	stub := &stubHostServiceClient{
		getConfigResp: &GetConfigResponse{Value: "v", Found: true},
	}
	h := &hostClient{client: stub, pluginName: "my-plugin"}

	val, found, err := h.GetConfig(ctx, "key")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "v", val)
	require.Equal(t, "my-plugin", stub.getConfigReq.GetPluginName())
	require.Equal(t, "key", stub.getConfigReq.GetKey())
}

// -- 9. PluginLogger + WithError -----------------------------------------

func TestPluginLogger(t *testing.T) {
	ctx := context.Background()

	t.Run("nil client returns error", func(t *testing.T) {
		pl := NewPluginLogger(nil)
		err := pl.Info(ctx, "msg", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no gRPC client available")
	})

	t.Run("level mapping", func(t *testing.T) {
		for _, tc := range []struct {
			level string
		}{
			{level: "debug"},
			{level: "info"},
			{level: "warn"},
			{level: "error"},
		} {
			t.Run(tc.level, func(t *testing.T) {
				stub := &stubExtensionServiceClient{}
				pl := NewPluginLogger(stub)
				log := map[string]func(context.Context, string, map[string]string) error{
					"debug": pl.Debug,
					"info":  pl.Info,
					"warn":  pl.Warn,
					"error": pl.Error,
				}[tc.level]
				msg := tc.level + "-msg"
				fields := map[string]string{"k": "v"}

				require.NoError(t, log(ctx, msg, fields))
				require.Equal(t, tc.level, stub.logReq.GetEntry().GetLevel())
				require.Equal(t, msg, stub.logReq.GetEntry().GetMessage())
				require.Equal(t, fields, stub.logReq.GetEntry().GetFields())
			})
		}
	})

	t.Run("RPC error propagated", func(t *testing.T) {
		stub := &stubExtensionServiceClient{logErr: errors.New("gRPC down")}
		pl := NewPluginLogger(stub)
		err := pl.Info(ctx, "msg", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "gRPC down")
	})
}

func TestWithError(t *testing.T) {
	t.Run("nil map creates new", func(t *testing.T) {
		got := WithError(nil, errors.New("oops"))
		require.Equal(t, map[string]string{"error": "oops"}, got)
	})

	t.Run("nil error is no-op", func(t *testing.T) {
		got := WithError(map[string]string{"k": "v"}, nil)
		require.Equal(t, map[string]string{"k": "v"}, got)
	})

	t.Run("nil map + nil error = empty map", func(t *testing.T) {
		got := WithError(nil, nil)
		require.NotNil(t, got)
		require.Empty(t, got)
	})

	t.Run("appends to existing map", func(t *testing.T) {
		got := WithError(map[string]string{"a": "1"}, errors.New("fail"))
		require.Equal(t, map[string]string{"a": "1", "error": "fail"}, got)
	})
}

// -- 10. Real transport smoke test via bufconn ---------------------------

func TestExtensionPluginRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	ext := &stubExtension{
		name:           "test-plugin",
		commands:       []*Command{{Name: "greet", Description: "says hi", ExtensionName: "test"}},
		runCommandOut:  "hello",
		runCommandView: &View{Id: "v1", Title: "Result"},
		reloadDiags:    []*Diagnostic{{Path: "cfg", Severity: "info", Message: "loaded"}},
		reloadCommands: []*Command{{Name: "greet"}},
		toolsOut: []*ToolDefinition{{
			Name:        "search",
			Description: "full-text search",
			InputSchema: `{"type":"object","properties":{"q":{"type":"string"}}}`,
		}},
		executeToolContent: `["result"]`,
		dispatchEventResp:  &EventResponse{InjectSystemPrompt: "sys"},
		capabilities:       []string{CapabilityCommands, CapabilityTools},
		docs:               "# Test Plugin",
	}

	// bufconn listener - in-memory gRPC.
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	RegisterExtensionServiceServer(srv, &GRPCServer{Impl: ext})
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		require.NoError(t, <-serveDone)
	})

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	client := &GRPCClient{Client: NewExtensionServiceClient(conn)}

	// Metadata.
	meta, err := client.Client.GetMetadata(ctx, &GetMetadataRequest{})
	require.NoError(t, err)
	require.Equal(t, "test-plugin", meta.GetName())
	require.Len(t, meta.GetCommands(), 1)
	require.Equal(t, "# Test Plugin", meta.GetDocs())

	// Capabilities.
	caps, err := client.Capabilities(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"commands", "tools"}, caps)

	// RunCommand with view round-trip.
	out, view, err := client.RunExtensionCommand(ctx, "greet", "Sam", nil)
	require.NoError(t, err)
	require.Equal(t, "hello", out)
	require.Equal(t, "greet", ext.runCommandName)
	require.Equal(t, "Sam", ext.runCommandArgs)
	require.NotNil(t, view)
	require.Equal(t, "v1", view.ID)
	require.Equal(t, "Result", view.Title)

	// ReloadExtensions.
	res, err := client.ReloadExtensions(ctx, false)
	require.NoError(t, err)
	require.Len(t, res.Diagnostics, 1)
	require.Equal(t, "loaded", res.Diagnostics[0].Message)

	// Tools.
	tools, err := client.Client.GetTools(ctx, &GetToolsRequest{})
	require.NoError(t, err)
	require.Len(t, tools.GetTools(), 1)
	require.Equal(t, "search", tools.GetTools()[0].GetName())
	executed, err := client.Client.ExecuteTool(ctx, &ExecuteToolRequest{
		ToolName:  "search",
		Arguments: `{"q":"tau"}`,
	})
	require.NoError(t, err)
	require.Equal(t, `["result"]`, executed.GetContent())
	require.False(t, executed.GetIsError())
	require.Equal(t, "search", ext.executeToolName)
	require.Equal(t, `{"q":"tau"}`, ext.executeToolArgs)

	// DispatchEvent.
	disp, err := client.Client.DispatchEvent(ctx, &DispatchEventRequest{
		Event:     "session_start",
		SessionId: "session-1",
		Payload: &EventPayload{
			Kind: &EventPayload_Session{Session: &SessionEventPayload{SessionId: "session-1"}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "sys", disp.GetResponse().GetInjectSystemPrompt())
	require.Equal(t, "session_start", ext.dispatchEventName)
	require.Equal(t, "session-1", ext.dispatchEventSessionID)
	require.Equal(t, "session-1", ext.dispatchEventPayload.GetSession().GetSessionId())
}
