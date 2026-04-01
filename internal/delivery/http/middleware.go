package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
)

// LoggerMiddleware logs incoming requests.
func LoggerMiddleware() func(http.Handler) http.Handler {
	log := logger.For("delivery", "http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(wrapped, r)

			log.Infof("%s %s %d (%.2fms)",
				r.Method,
				r.RequestURI,
				wrapped.Status(),
				float64(time.Since(start).Microseconds())/1000.0,
			)
		})
	}
}

// CORSMiddleware allows cross-origin requests during development.
func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Internal-Key, X-App-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MetricsMiddleware records HTTP request duration for Prometheus.
func MetricsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(wrapped, r)

			metrics.HTTPRequestDuration.WithLabelValues(
				r.Method,
				r.URL.Path,
				fmt.Sprintf("%d", wrapped.Status()),
			).Observe(time.Since(start).Seconds())
		})
	}
}

// BodyLimitMiddleware restricts the maximum request body size.
// Requests exceeding maxBytes receive HTTP 413 Request Entity Too Large.
func BodyLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.ContentLength != 0 {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AppIDMiddleware extracts the X-App-ID header and injects it into the request context.
// Returns 400 if the header is missing or empty.
func AppIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appID := r.Header.Get("X-App-ID")
			if appID == "" {
				respondError(w, http.StatusBadRequest, "missing_app_id")
				return
			}
			ctx := domain.WithAppID(r.Context(), appID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// InternalAPIKeyMiddleware validates the X-Internal-Key header for service-to-service calls.
// Returns 401 if the key is missing or does not match the configured secret.
func InternalAPIKeyMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Internal-Key")
			if key == "" || key != apiKey {
				respondError(w, http.StatusUnauthorized, "invalid_internal_key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RecoverMiddleware recovers from panics.
func RecoverMiddleware() func(http.Handler) http.Handler {
	log := logger.For("delivery", "http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Errorf("Panic recovered: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
