package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server is the MCP control server (Model Context Protocol endpoint facade).
type Server struct {
	srv *http.Server
	r   chi.Router
}

// New creates a new MCP server.
func New(cfg *config.MCPConfig) *Server {
	r := chi.NewRouter()

	// Basic middleware
	r.Use(CORSMiddleware)
	r.Use(LoggerMiddleware)
	r.Use(RecoverMiddleware)

	mcpAddr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         mcpAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Server{srv: srv, r: r}
}

// Router returns the underlying chi router.
func (s *Server) Router() chi.Router {
	return s.r
}

// RegisterRoutes attaches MCP-compatible endpoints.
func (s *Server) RegisterRoutes() {
	// API discovery
	s.r.Get("/.well-known/mcp", WellKnownHandler())
	// Health endpoints
	s.r.Get("/health", HealthHandler())
	s.r.Get("/ready", ReadyHandler())
	// Metrics and docs
	s.r.Handle("/metrics", promhttp.Handler())
	s.r.Get("/docs/openapi.yaml", OpenAPISpecHandler())
}

// Start runs the server (blocking).
func (s *Server) Start() error {
	log := logger.For("delivery", "mcp")
	log.Infof("MCP server starting on %s", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Handlers

func WellKnownHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":            "nexus-mcp",
			"description":     "Nexus MCP endpoint",
			"version":         "1.0.0",
			"openapi":         "/docs/openapi.yaml",
			"health":          "/health",
			"readiness":       "/ready",
			"supported_paths": []string{
				"/internal/v1/auth/register",
				"/v1/messages/send",
				"/v1/messages/edit",
				"/v1/messages/delete",
				"/v1/messages/ack",
				"/v1/groups/create",
				"/v1/groups/detail",
				"/v1/groups/update",
				"/v1/groups/join",
				"/v1/groups/leave",
				"/v1/profile",
				"/v1/history",
			},
		}) //nolint:errcheck
	}
}

func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}
}

func ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"}) //nolint:errcheck
	}
}

func OpenAPISpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		http.ServeFile(w, r, "docs/openapi.yaml")
	}
}

// Middleware wrappers

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.For("delivery", "mcp")
		log.Infof("MCP request %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.For("delivery", "mcp").Errorf("panic recovered: %v", rec)
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{"error": "internal_server_error"}) //nolint:errcheck
			}
		}()
		next.ServeHTTP(w, r)
	})
}
