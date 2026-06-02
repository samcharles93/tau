// Package plugin provides the host-side plugin manager for tau.
// Plugin binaries communicate via gRPC using the shared types in pkg/plugin/api.
package plugin

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"google.golang.org/grpc"
)

// ExtensionPlugin is the go-plugin shim that adapts an api.Extension to gRPC.
type ExtensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl api.Extension
}

func (p *ExtensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	api.RegisterExtensionServiceServer(s, &api.GRPCServer{Impl: p.Impl})
	return nil
}

func (p *ExtensionPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &api.GRPCClient{Client: api.NewExtensionServiceClient(c)}, nil
}
