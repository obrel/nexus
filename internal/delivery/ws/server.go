package ws

import (
	"context"
	"fmt"
	"net/http"

	"github.com/obrel/nexus/internal/infra/logger"
)

// Server wraps net/http.Server for the WebSocket egress tier.
type Server struct {
	srv *http.Server
	log logger.Logger
}

// NewServer creates a Server that routes all requests to handler.
func NewServer(port int, handler http.Handler) *Server {
	return &Server{
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: handler,
		},
		log: logger.For("delivery", "ws"),
	}
}

// Start begins accepting connections. Blocks until the server stops.
func (s *Server) Start() error {
	s.log.Infof("WebSocket server listening on %s", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully drains in-flight requests within the deadline of ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
