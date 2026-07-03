package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMCPServer exercises the plugin against a real MCP server end to end:
// it connects, lists the server's tools (proving the Tools() bridge), and can
// optionally invoke one (proving ExecuteTool). It is gated behind MCP_LIVE_URL
// so a normal `go test` run skips it.
//
// List tools from a Streamable HTTP server:
//
//	MCP_LIVE_URL=http://localhost:9343/mcp go test -run TestLiveMCPServer -v .
//
// Also call a tool (the plugin's "<server>.<tool>" name, args are JSON):
//
//	MCP_LIVE_URL=http://localhost:9343/mcp \
//	MCP_LIVE_CALL=spawn.list_agents MCP_LIVE_ARGS='{}' \
//	go test -run TestLiveMCPServer -v .
func TestLiveMCPServer(t *testing.T) {
	url := os.Getenv("MCP_LIVE_URL")
	if url == "" {
		t.Skip("set MCP_LIVE_URL to run the live MCP integration test")
	}
	name := os.Getenv("MCP_LIVE_NAME")
	if name == "" {
		name = "spawn"
	}

	cfg, err := json.Marshal([]serverConfig{{Name: name, URL: url}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TAU_MCP_SERVERS", string(cfg))

	p := &MCPPlugin{
		sessions: make(map[string]*mcpSession),
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools, err := p.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	if len(tools) == 0 {
		t.Fatalf("connected to %q (%s) but it advertised no tools", name, url)
	}
	t.Logf("connected to %q — %d tool(s):", name, len(tools))
	for _, tl := range tools {
		t.Logf("  %s — %s", tl.Name, firstLine(tl.Description))
	}

	call := os.Getenv("MCP_LIVE_CALL")
	if call == "" {
		return
	}
	args := os.Getenv("MCP_LIVE_ARGS")
	if args == "" {
		args = "{}"
	}
	content, isErr, err := p.ExecuteTool(ctx, call, args)
	if err != nil {
		t.Fatalf("ExecuteTool(%q) transport error: %v", call, err)
	}
	// A tool-level error (isErr==true) still proves the round trip works.
	t.Logf("ExecuteTool(%q) isError=%v content=%s", call, isErr, truncate(content, 800))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
