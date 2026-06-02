package api

import (
	"context"

	"github.com/samcharles93/tau/internal/chat"
)

// GRPCServer adapts an Extension implementation to the gRPC service.
type GRPCServer struct {
	UnimplementedExtensionServiceServer
	Impl Extension
}

func (s *GRPCServer) GetMetadata(ctx context.Context, req *GetMetadataRequest) (*GetMetadataResponse, error) {
	name, cmds := s.Impl.Metadata()
	return &GetMetadataResponse{Name: name, Commands: cmds}, nil
}

func (s *GRPCServer) RunCommand(ctx context.Context, req *RunCommandRequest) (*RunCommandResponse, error) {
	output, err := s.Impl.RunCommand(ctx, req.Name, req.Args)
	if err != nil {
		return nil, err
	}
	return &RunCommandResponse{Output: output}, nil
}

func (s *GRPCServer) Reload(ctx context.Context, req *ReloadRequest) (*ReloadResponse, error) {
	diagnostics, cmds, err := s.Impl.Reload(ctx)
	if err != nil {
		return nil, err
	}
	return &ReloadResponse{Diagnostics: diagnostics, Commands: cmds}, nil
}

func (s *GRPCServer) DispatchEvent(ctx context.Context, req *DispatchEventRequest) (*DispatchEventResponse, error) {
	resp := s.Impl.DispatchEvent(ctx, req.Event, req.Payload)
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
	content, isError, err := s.Impl.ExecuteTool(ctx, req.ToolName, req.Arguments)
	if err != nil {
		return nil, err
	}
	return &ExecuteToolResponse{Content: content, IsError: isError}, nil
}

// GRPCClient adapts the gRPC client to the chat.ExtensionReloader interface.
type GRPCClient struct {
	Client ExtensionServiceClient
	cmds   []chat.ExtensionCommand
}

var _ chat.ExtensionReloader = &GRPCClient{}

func (c *GRPCClient) ReloadExtensions(ctx context.Context, idle bool) (chat.ExtensionReloadResult, error) {
	resp, err := c.Client.Reload(ctx, &ReloadRequest{})
	if err != nil {
		return chat.ExtensionReloadResult{}, err
	}
	c.cmds = protoCommandsToChat(resp.Commands)
	return chat.ExtensionReloadResult{
		ExtensionCount: 1,
		Diagnostics:    protoDiagnosticsToChat(resp.Diagnostics),
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
	c.cmds = protoCommandsToChat(resp.Commands)
	return c.cmds
}

func (c *GRPCClient) RunExtensionCommand(ctx context.Context, name, args string, uiBridge any) (string, error) {
	resp, err := c.Client.RunCommand(ctx, &RunCommandRequest{Name: name, Args: args})
	if err != nil {
		return "", err
	}
	return resp.Output, nil
}

func protoCommandsToChat(cmds []*Command) []chat.ExtensionCommand {
	out := make([]chat.ExtensionCommand, len(cmds))
	for i, c := range cmds {
		out[i] = chat.ExtensionCommand{
			Name:          c.Name,
			Description:   c.Description,
			ExtensionName: c.ExtensionName,
		}
	}
	return out
}

func protoDiagnosticsToChat(diags []*Diagnostic) []chat.ExtensionDiagnostic {
	out := make([]chat.ExtensionDiagnostic, len(diags))
	for i, d := range diags {
		out[i] = chat.ExtensionDiagnostic{
			Path:          d.Path,
			ExtensionName: d.ExtensionName,
			Severity:      d.Severity,
			Message:       d.Message,
		}
	}
	return out
}
