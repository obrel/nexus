package http

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// Claims represents JWT payload with tenant and user identification.
type Claims struct {
	Sub string `json:"sub"` // user_id
	Tid string `json:"tid"` // tenant_id / app_id
	jwt.RegisteredClaims
}

// AuthMiddleware validates JWT and injects tenant/user context.
// Returns 401 if JWT is missing, invalid, or claims are incomplete.
func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	log := logger.For("delivery", "http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract Bearer token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Warnf("Missing Authorization header: %s %s", r.Method, r.RequestURI)
				respondError(w, http.StatusUnauthorized, "missing_authorization")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				log.Warnf("Invalid Authorization header format: %s %s", r.Method, r.RequestURI)
				respondError(w, http.StatusUnauthorized, "invalid_authorization")
				return
			}

			tokenString := parts[1]

			// Parse JWT
			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
				// Verify signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})

			if err != nil || !token.Valid {
				log.Warnf("Invalid JWT: %v (%s %s)", err, r.Method, r.RequestURI)
				respondError(w, http.StatusUnauthorized, "invalid_token")
				return
			}

			// Validate required claims
			if claims.Sub == "" || claims.Tid == "" {
				log.Warnf("Missing required JWT claims: sub=%q tid=%q (%s %s)", claims.Sub, claims.Tid, r.Method, r.RequestURI)
				respondError(w, http.StatusUnauthorized, "incomplete_claims")
				return
			}

			// Inject into request context
			ctx := r.Context()
			ctx = domain.WithAppID(ctx, claims.Tid)
			ctx = domain.WithUserID(ctx, claims.Sub)

			log.Debugf("Authenticated: user=%s tenant=%s", claims.Sub, claims.Tid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
