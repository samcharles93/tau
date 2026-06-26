// Package server provides the HTTP and WebSocket surface for the Tau Web UI.
// It serves the embedded SPA and upgrades /ws to the bridge.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Bridge is the subset of internal/bridge.Bridge that the server needs.
type Bridge interface {
	UpgradeHTTP(w http.ResponseWriter, r *http.Request) error
	ClientCount() int
	Close() error
}

// Server serves the embedded SPA and the /ws WebSocket endpoint.
type Server struct {
	addr    string
	bridge  Bridge
	spa     http.Handler
	logger  *slog.Logger
	httpSrv *http.Server
	mu      sync.Mutex
	wg      sync.WaitGroup
}

// New creates a server that binds to the given address. Use ":0" to let the
// OS assign a free port.
func New(addr string, bridge Bridge, spa http.Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if spa == nil {
		spa = http.NotFoundHandler()
	}
	return &Server{
		addr:   addr,
		bridge: bridge,
		spa:    spa,
		logger: logger,
	}
}

// Start binds the listener, prints the reachable URL, and serves until ctx is
// cancelled. It returns immediately with the bound address; serving runs in a
// background goroutine.
func (s *Server) Start(ctx context.Context) (string, error) {
	bindAddr := s.localAddr()

	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", bindAddr)
	if err != nil {
		return "", fmt.Errorf("listen %s: %w", bindAddr, err)
	}
	bound := ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ws", s.handleWebSocket)
	mux.Handle("/", s.spa)

	s.mu.Lock()
	s.httpSrv = &http.Server{
		Addr:    bound,
		Handler: mux,
	}
	s.mu.Unlock()

	s.wg.Go(func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Warn("serve", "err", err)
		}
	})

	go func() {
		<-ctx.Done()
		s.shutdown()
	}()

	s.logger.Info("web ui listening", "url", "http://"+bound)
	return bound, nil
}

// Wait blocks until the server has fully shut down.
func (s *Server) Wait() {
	s.wg.Wait()
}

func (s *Server) localAddr() string {
	bindAddr := s.addr
	if bindAddr == "" {
		return "127.0.0.1:0"
	}
	if !strings.Contains(bindAddr, ":") {
		return "127.0.0.1:" + bindAddr
	}
	if strings.HasPrefix(bindAddr, ":") {
		return "127.0.0.1" + bindAddr
	}
	return bindAddr
}

// URL returns the reachable HTTP URL once the server has started.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpSrv == nil {
		return ""
	}
	return "http://" + s.httpSrv.Addr
}

func (s *Server) shutdown() {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("web server shutdown", "err", err)
	}
	if err := s.bridge.Close(); err != nil {
		s.logger.Warn("bridge shutdown", "err", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"clients": s.bridge.ClientCount(),
	})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if err := s.bridge.UpgradeHTTP(w, r); err != nil {
		s.logger.Warn("websocket upgrade failed", "err", err)
	}
}
