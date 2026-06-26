package app

import (
	"context"
	"fmt"
	"log/slog"

	webbridge "github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	webserver "github.com/samcharles93/tau/internal/server"
	"github.com/samcharles93/tau/internal/spa"
)

// webServerResult holds the started web UI server and its reachable URL.
type webServerResult struct {
	Server   *webserver.Server
	URL      string
	Shutdown func()
	Wait     func()
}

// startWebUI starts the optional Web UI bridge and HTTP server. It returns the
// server result and the reachable URL, or empty values if the web UI is disabled
// or fails to start. The caller is responsible for calling Shutdown/Wait.
func startWebUI(
	runtime webbridge.Runtime,
	bus *eventbus.Bus,
	opts ChatOptions,
	sessionID string,
	modelID string,
	commands []tauchat.CommandRef,
	logger *slog.Logger,
) (*webServerResult, error) {
	if opts.NoWeb {
		return nil, nil
	}

	bridge, err := webbridge.NewBridge(runtime, bus, webbridge.InitInfo{
		SessionID: sessionID,
		Model:     modelID,
		Provider:  opts.Provider.Name,
		Commands:  commands,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("web bridge: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", opts.WebPort)
	if opts.WebPort == 0 {
		addr = "127.0.0.1:0"
	}

	webCtx, webCancel := context.WithCancel(context.Background())
	webSrv := webserver.New(addr, bridge, spa.Handler(), logger)
	bound, err := webSrv.Start(webCtx)
	if err != nil {
		webCancel()
		return nil, fmt.Errorf("web server: %w", err)
	}

	url := "http://" + bound
	return &webServerResult{
		Server:   webSrv,
		URL:      url,
		Shutdown: webCancel,
		Wait:     webSrv.Wait,
	}, nil
}
