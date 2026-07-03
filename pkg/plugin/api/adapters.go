package api

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/chat"
)

// GRPCServer adapts an Extension implementation to the gRPC service.
type GRPCServer struct {
	UnimplementedExtensionServiceServer
	Impl   Extension
	broker *plugin.GRPCBroker
}

// Init receives the host's HostService broker id, dials it, and hands the
// resulting Host to the Extension if it is HostAware.
func (s *GRPCServer) Init(ctx context.Context, req *InitRequest) (*InitResponse, error) {
	if s.broker == nil || req.GetHostBrokerId() == 0 {
		return &InitResponse{}, nil
	}
	conn, err := s.broker.Dial(req.GetHostBrokerId())
	if err != nil {
		return nil, err
	}
	host := &hostClient{client: NewHostServiceClient(conn), pluginName: req.GetPluginName()}
	if aware, ok := s.Impl.(HostAware); ok {
		aware.SetHost(host)
	}
	return &InitResponse{}, nil
}

// GetCapabilities reports the plugin's advertised capabilities, defaulting to
// the full legacy surface when the Extension does not implement Capable.
func (s *GRPCServer) GetCapabilities(ctx context.Context, req *GetCapabilitiesRequest) (*GetCapabilitiesResponse, error) {
	if c, ok := s.Impl.(Capable); ok {
		return &GetCapabilitiesResponse{Capabilities: c.Capabilities()}, nil
	}
	return &GetCapabilitiesResponse{Capabilities: []string{CapabilityCommands, CapabilityTools, CapabilityEvents}}, nil
}

func (s *GRPCServer) GetMetadata(ctx context.Context, req *GetMetadataRequest) (*GetMetadataResponse, error) {
	name, cmds := s.Impl.Metadata()
	return &GetMetadataResponse{Name: name, Commands: cmds}, nil
}

func (s *GRPCServer) RunCommand(ctx context.Context, req *RunCommandRequest) (*RunCommandResponse, error) {
	output, view, err := s.Impl.RunCommand(ctx, req.GetName(), req.GetArgs())
	if err != nil {
		return nil, err
	}
	return &RunCommandResponse{Output: output, View: view}, nil
}

func (s *GRPCServer) Reload(ctx context.Context, req *ReloadRequest) (*ReloadResponse, error) {
	diagnostics, cmds, err := s.Impl.Reload(ctx)
	if err != nil {
		return nil, err
	}
	return &ReloadResponse{Diagnostics: diagnostics, Commands: cmds}, nil
}

func (s *GRPCServer) DispatchEvent(ctx context.Context, req *DispatchEventRequest) (*DispatchEventResponse, error) {
	resp := s.Impl.DispatchEvent(ctx, req.GetEvent(), req.GetSessionId(), req.GetPayload())
	return &DispatchEventResponse{Response: resp}, nil
}

func (s *GRPCServer) GetTools(ctx context.Context, req *GetToolsRequest) (*GetToolsResponse, error) {
	tools, err := s.Impl.Tools(ctx)
	if err != nil {
		return nil, err
	}
	return &GetToolsResponse{Tools: tools}, nil
}

func (s *GRPCServer) ExecuteTool(ctx context.Context, req *ExecuteToolRequest) (*ExecuteToolResponse, error) {
	content, isError, err := s.Impl.ExecuteTool(ctx, req.GetToolName(), req.GetArguments())
	if err != nil {
		return nil, err
	}
	return &ExecuteToolResponse{Content: content, IsError: isError}, nil
}

// GRPCClient adapts the gRPC client to the chat.ExtensionReloader interface.
type GRPCClient struct {
	Client       ExtensionServiceClient
	cmds         []chat.ExtensionCommand
	hostBrokerID uint32
}

var _ chat.ExtensionReloader = &GRPCClient{}

// Capabilities returns the plugin's advertised capability set.
func (c *GRPCClient) Capabilities(ctx context.Context) ([]string, error) {
	resp, err := c.Client.GetCapabilities(ctx, &GetCapabilitiesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetCapabilities(), nil
}

// Init tells the plugin which broker id to dial for the host's HostService.
// It is a no-op when no HostService is being served.
func (c *GRPCClient) Init(ctx context.Context, pluginName string) error {
	if c.hostBrokerID == 0 {
		return nil
	}
	_, err := c.Client.Init(ctx, &InitRequest{HostBrokerId: c.hostBrokerID, PluginName: pluginName})
	return err
}

func (c *GRPCClient) ReloadExtensions(ctx context.Context, idle bool) (chat.ExtensionReloadResult, error) {
	resp, err := c.Client.Reload(ctx, &ReloadRequest{})
	if err != nil {
		return chat.ExtensionReloadResult{}, err
	}
	c.cmds = protoCommandsToChat(resp.GetCommands())
	return chat.ExtensionReloadResult{
		ExtensionCount: 1,
		Diagnostics:    protoDiagnosticsToChat(resp.GetDiagnostics()),
		Commands:       c.cmds,
	}, nil
}

func (c *GRPCClient) ExtensionCommands() []chat.ExtensionCommand {
	if c.cmds != nil {
		return c.cmds
	}
	resp, err := c.Client.GetMetadata(context.Background(), &GetMetadataRequest{})
	if err != nil {
		return nil
	}
	c.cmds = protoCommandsToChat(resp.GetCommands())
	return c.cmds
}

func (c *GRPCClient) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, *chat.ExtensionView, error) {
	resp, err := c.Client.RunCommand(ctx, &RunCommandRequest{Name: name, Args: args})
	if err != nil {
		return "", nil, err
	}
	return resp.GetOutput(), ProtoViewToChat(resp.GetView()), nil
}

func protoCommandsToChat(cmds []*Command) []chat.ExtensionCommand {
	out := make([]chat.ExtensionCommand, len(cmds))
	for i, c := range cmds {
		out[i] = chat.ExtensionCommand{
			Name:          c.GetName(),
			Description:   c.GetDescription(),
			ExtensionName: c.GetExtensionName(),
			ArgsHint:      c.GetArgsHint(),
			Subcommands:   protoCommandsToChat(c.GetSubcommands()),
		}
	}
	return out
}

func protoDiagnosticsToChat(diags []*Diagnostic) []chat.ExtensionDiagnostic {
	out := make([]chat.ExtensionDiagnostic, len(diags))
	for i, d := range diags {
		out[i] = chat.ExtensionDiagnostic{
			Path:          d.GetPath(),
			ExtensionName: d.GetExtensionName(),
			Severity:      d.GetSeverity(),
			Message:       d.GetMessage(),
		}
	}
	return out
}

// ProtoViewToChat converts a wire View into the internal/chat domain type.
// Exported because both this package's own adapters and internal/agent's
// HostService-facing bridge need it (a plugin pushes a View to HostService,
// which forwards the raw proto message up to the bridge for conversion).
func ProtoViewToChat(v *View) *chat.ExtensionView {
	if v == nil {
		return nil
	}
	widgets := make([]chat.Widget, 0, len(v.GetWidgets()))
	for _, w := range v.GetWidgets() {
		widgets = append(widgets, ProtoWidgetToChat(w))
	}
	return &chat.ExtensionView{
		ID:      v.GetId(),
		Title:   v.GetTitle(),
		Widgets: widgets,
		Style:   protoStyleToChat(v.GetStyle()),
	}
}

// ProtoWidgetToChat converts a wire Widget into the internal/chat domain
// type. Unrecognized/unset oneof kinds (e.g. from a newer plugin talking to
// an older host) map to a zero-value chat.Widget, which renders as nothing -
// the same additive-evolution guarantee as the proto oneof itself.
func ProtoWidgetToChat(w *Widget) chat.Widget {
	if w == nil {
		return chat.Widget{}
	}
	switch k := w.GetKind().(type) {
	case *Widget_Text:
		return chat.Widget{
			Kind: chat.WidgetKindText,
			Text: &chat.TextWidget{Text: k.Text.GetText(), Style: protoStyleToChat(k.Text.GetStyle())},
		}
	case *Widget_Stack:
		children := make([]chat.Widget, 0, len(k.Stack.GetChildren()))
		for _, c := range k.Stack.GetChildren() {
			children = append(children, ProtoWidgetToChat(c))
		}
		direction := chat.StackVertical
		if k.Stack.GetDirection() == StackWidget_HORIZONTAL {
			direction = chat.StackHorizontal
		}
		return chat.Widget{
			Kind: chat.WidgetKindStack,
			Stack: &chat.StackWidget{
				Direction: direction,
				Children:  children,
				Gap:       int(k.Stack.GetGap()),
			},
		}
	case *Widget_KeyValue:
		entries := make([]chat.KeyValueEntry, 0, len(k.KeyValue.GetEntries()))
		for _, e := range k.KeyValue.GetEntries() {
			entries = append(entries, chat.KeyValueEntry{
				Key:        e.GetKey(),
				Value:      e.GetValue(),
				ValueStyle: protoStyleToChat(e.GetValueStyle()),
			})
		}
		return chat.Widget{Kind: chat.WidgetKindKeyValue, KeyValue: &chat.KeyValueWidget{Entries: entries}}
	case *Widget_List:
		return chat.Widget{
			Kind: chat.WidgetKindList,
			List: &chat.ListWidget{
				Items:   k.List.GetItems(),
				Ordered: k.List.GetOrdered(),
				Style:   protoStyleToChat(k.List.GetStyle()),
			},
		}
	case *Widget_Table:
		rows := make([]chat.TableRow, 0, len(k.Table.GetRows()))
		for _, r := range k.Table.GetRows() {
			rows = append(rows, chat.TableRow{Cells: r.GetCells()})
		}
		return chat.Widget{
			Kind:  chat.WidgetKindTable,
			Table: &chat.TableWidget{Headers: k.Table.GetHeaders(), Rows: rows},
		}
	case *Widget_Progress:
		return chat.Widget{
			Kind: chat.WidgetKindProgress,
			Progress: &chat.ProgressWidget{
				Label:    k.Progress.GetLabel(),
				Fraction: k.Progress.GetFraction(),
				Style:    protoStyleToChat(k.Progress.GetStyle()),
			},
		}
	case *Widget_Divider:
		return chat.Widget{Kind: chat.WidgetKindDivider, Divider: &chat.DividerWidget{Label: k.Divider.GetLabel()}}
	case *Widget_Status:
		state := chat.StatusRunning
		switch k.Status.GetState() {
		case StatusWidget_SUCCESS:
			state = chat.StatusSuccess
		case StatusWidget_FAILED:
			state = chat.StatusFailed
		case StatusWidget_NEUTRAL:
			state = chat.StatusNeutral
		}
		return chat.Widget{
			Kind: chat.WidgetKindStatus,
			Status: &chat.StatusWidget{
				State:  state,
				Label:  k.Status.GetLabel(),
				Detail: k.Status.GetDetail(),
			},
		}
	default:
		return chat.Widget{}
	}
}

func protoStyleToChat(s *Style) *chat.Style {
	if s == nil {
		return nil
	}
	tone := chat.ToneDefault
	switch s.GetTone() {
	case Style_TONE_INFO:
		tone = chat.ToneInfo
	case Style_TONE_SUCCESS:
		tone = chat.ToneSuccess
	case Style_TONE_WARN:
		tone = chat.ToneWarn
	case Style_TONE_ERROR:
		tone = chat.ToneError
	case Style_TONE_MUTED:
		tone = chat.ToneMuted
	}
	return &chat.Style{
		Tone:      tone,
		FgHex:     s.GetFgHex(),
		BgHex:     s.GetBgHex(),
		Bold:      s.GetBold(),
		Dim:       s.GetDim(),
		Italic:    s.GetItalic(),
		Underline: s.GetUnderline(),
	}
}
