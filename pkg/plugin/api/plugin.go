// Package api provides the shared types, interfaces, and gRPC adapters
// for tau's plugin system. Both the tau host and plugin binaries compile
// against this package.
package api

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// Extension is the interface that plugin binaries must implement.
type Extension interface {
	Metadata() (name string, commands []*Command)
	RunCommand(ctx context.Context, name, args string) (string, error)
	Reload(ctx context.Context) (diagnostics []*Diagnostic, commands []*Command, err error)
	Tools(ctx context.Context) ([]*ToolDefinition, error)
	ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error)
	DispatchEvent(ctx context.Context, event string, payload *EventPayload) *EventResponse
}

// ExtensionPlugin is the go-plugin shim that adapts an Extension to gRPC.
type ExtensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl Extension
}

func (p *ExtensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	RegisterExtensionServiceServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

func (p *ExtensionPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	return &GRPCClient{Client: NewExtensionServiceClient(c)}, nil
}
