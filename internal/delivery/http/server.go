package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server is the HTTP Ingress server for Nexus.
type Server struct {
	srv            *http.Server
	r              chi.Router
	jwtSecret      string
	internalAPIKey string
	internalRoutes bool
}

// New creates a new HTTP Server. The router is available for attaching handlers and middleware.
func New(cfg *config.HTTPConfig) *Server {
	r := chi.NewRouter()

	// Global middleware (applied to all routes including auth)
	r.Use(CORSMiddleware())
	r.Use(BodyLimitMiddleware(cfg.MaxBodySize))
	r.Use(MetricsMiddleware())
	r.Use(LoggerMiddleware())
	r.Use(RecoverMiddleware())

	maxHeaderBytes := cfg.MaxHeaderBytes
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = 1 << 20
	}

	srv := &http.Server{
		Addr:           fmt.Sprintf(":%d", cfg.Port),
		Handler:        r,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
	}

	return &Server{
		srv:            srv,
		r:              r,
		jwtSecret:      cfg.JWTSecret,
		internalAPIKey: cfg.InternalAPIKey,
		internalRoutes: cfg.InternalRoutes,
	}
}

// Router returns the underlying chi router for route registration.
func (s *Server) Router() chi.Router {
	return s.r
}

// RegisterRoutes registers HTTP handlers for all Ingress use cases.
func (s *Server) RegisterRoutes(
	msgUC usecase.MessageUseCase,
	grpUC usecase.GroupUseCase,
	profileUC usecase.ProfileUseCase,
	historyUC usecase.HistoryUseCase,
	authUC usecase.AuthUseCase,
	eventUC usecase.EventUseCase,
	defaultAppID string,
	healthDeps *HealthDeps,
) {
	// Operational endpoints (no auth)
	s.r.Get("/health", HealthHandler())
	s.r.Get("/ready", ReadyHandler(healthDeps))
	s.r.Handle("/metrics", promhttp.Handler())

	// API documentation
	s.r.Get("/docs", SwaggerUIHandler())
	s.r.Get("/docs/openapi.yaml", OpenAPISpecHandler())

	// Internal routes (API key required) — service-to-service
	if s.internalRoutes && s.internalAPIKey != "" {
		s.r.Group(func(r chi.Router) {
			r.Use(InternalAPIKeyMiddleware(s.internalAPIKey))
			r.Use(AppIDMiddleware())
			r.Post("/internal/v1/auth/register", NewInternalRegisterHandler(authUC, defaultAppID))
			r.Post("/internal/v1/messages/send", NewInternalMessageSendHandler(msgUC))
			r.Post("/internal/v1/messages/edit", NewInternalMessageEditHandler(msgUC))
			r.Post("/internal/v1/messages/delete", NewInternalMessageDeleteHandler(msgUC))
			r.Post("/internal/v1/messages/ack", NewInternalMessageAckHandler(msgUC))
			r.Post("/internal/v1/groups/create", NewInternalGroupCreateHandler(grpUC))
			r.Get("/internal/v1/groups/detail", NewInternalGroupGetHandler(grpUC))
			r.Post("/internal/v1/groups/update", NewInternalGroupUpdateHandler(grpUC))
			r.Post("/internal/v1/groups/join", NewInternalGroupJoinHandler(grpUC))
			r.Post("/internal/v1/groups/leave", NewInternalGroupLeaveHandler(grpUC))
		})
	}

	// Protected routes (JWT required)
	s.r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(s.jwtSecret))

		// Message lifecycle
		r.Post("/v1/messages/send", NewMessageSendHandler(msgUC))
		r.Post("/v1/messages/edit", NewMessageEditHandler(msgUC))
		r.Post("/v1/messages/delete", NewMessageDeleteHandler(msgUC))
		r.Post("/v1/messages/ack", NewMessageAckHandler(msgUC))

		// Group management
		r.Post("/v1/groups/create", NewGroupCreateHandler(grpUC))
		r.Get("/v1/groups/detail", NewGroupGetHandler(grpUC))
		r.Post("/v1/groups/update", NewGroupUpdateHandler(grpUC))
		r.Post("/v1/groups/join", NewGroupJoinHandler(grpUC))
		r.Post("/v1/groups/leave", NewGroupLeaveHandler(grpUC))

		// User profile
		r.Get("/v1/profile", NewProfileGetHandler(profileUC))
		r.Post("/v1/profile", NewProfileUpdateHandler(profileUC))

		// History sync
		r.Get("/v1/history", NewHistoryHandler(historyUC))

		// Custom events
		r.Post("/v1/events/send", NewEventSendHandler(eventUC))
	})
}

// Start begins listening on the configured port. This is blocking.
func (s *Server) Start() error {
	log := logger.For("delivery", "http")
	log.Infof("HTTP Ingress server starting on %s", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server with the given context.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
