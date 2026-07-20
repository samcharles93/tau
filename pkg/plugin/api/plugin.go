// Package api provides the shared types, interfaces, and gRPC adapters
// for tau's plugin system. Both the tau host and plugin binaries compile
// against this package.
package api

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// Handshake is the go-plugin handshake shared by the tau host and every
// plugin binary. ProtocolVersion gates wire compatibility: bump it whenever
// the proto contract changes incompatibly (package rename, field type change,
// enum renumbering) so stale plugin binaries fail cleanly at handshake
// instead of misbehaving mid-call.
//
// Version history:
//
//	1: original contract (proto package "proto")
//	2: proto package tau.plugin.v1; StackWidget.Direction/StatusWidget.State
//	   values renamed and State renumbered; dead fields removed
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  2,
	MagicCookieKey:   "TAU_PLUGIN",
	MagicCookieValue: "tau",
}

// Capability identifiers a plugin may advertise via GetCapabilities. The set is
// open: the host ignores unknown values, so new capability types can be added
// without breaking existing plugins.
const (
	CapabilityCommands    = "commands"
	CapabilityTools       = "tools"
	CapabilityEvents      = "events"
	CapabilityInteractive = "interactive"
	// CapabilityViews marks plugins that render structured panels via
	// RunCommand's view return value and/or Host.RenderView/CloseView. Unlike
	// the other capabilities, this is never assumed for legacy plugins that
	// don't implement Capable - it's a net-new UI surface a plugin must
	// deliberately opt into.
	CapabilityViews = "views"
)

// Capable is an optional interface for plugins to declare which capabilities
// they provide. Plugins that do not implement it are assumed to support the
// full legacy Extension surface (commands, tools, events).
type Capable interface {
	Capabilities() []string
}

// Documented is an optional interface for plugins to expose their own
// markdown documentation to the host. Tau's docs tool has no visibility into
// a plugin's source tree - it only knows about tau's own embedded docs - so
// a plugin that wants to be discoverable through that tool (e.g. "how do I
// configure the foo plugin?") must surface its docs this way. Docs is called
// once per load via GetMetadata; a plugin that ships no documentation simply
// omits this interface.
type Documented interface {
	Docs() string
}

// Extension is the interface that plugin binaries must implement.
type Extension interface {
	Metadata() (name string, commands []*Command)
	RunCommand(ctx context.Context, name, args string) (output string, view *View, err error)
	Reload(ctx context.Context) (diagnostics []*Diagnostic, commands []*Command, err error)
	Tools(ctx context.Context) ([]*ToolDefinition, error)
	ExecuteTool(ctx context.Context, toolName, arguments string) (content string, isError bool, err error)
	DispatchEvent(ctx context.Context, event string, sessionID string, payload *EventPayload) *EventResponse
}

// ExtensionPlugin is the go-plugin shim that adapts an Extension to gRPC.
type ExtensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl Extension
	// HostImpl, when set on the host side, is served to the plugin over the
	// go-plugin broker so the plugin can call back into tau via HostService.
	HostImpl HostServiceServer
}

func (p *ExtensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	RegisterExtensionServiceServer(s, &GRPCServer{Impl: p.Impl, broker: broker})
	return nil
}

func (p *ExtensionPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (any, error) {
	client := &GRPCClient{Client: NewExtensionServiceClient(c)}
	// Serve HostService back to the plugin over the broker. The plugin dials
	// the returned broker id once the host calls Init.
	if p.HostImpl != nil {
		brokerID := broker.NextId()
		host := p.HostImpl
		go broker.AcceptAndServe(brokerID, func(opts []grpc.ServerOption) *grpc.Server {
			s := grpc.NewServer(opts...)
			RegisterHostServiceServer(s, host)
			return s
		})
		client.hostBrokerID = brokerID
	}
	return client, nil
}
