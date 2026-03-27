package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/obrel/nexus/internal/domain"
)

const testSecret = "test-secret-key-very-secure"

func validToken(sub, tid string) string {
	claims := Claims{
		Sub: sub,
		Tid: tid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(testSecret))
	return tokenString
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	token := validToken("user123", "tenant-abc")
	middleware := AuthMiddleware(testSecret)

	nextCalled := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		// Verify context was populated
		if domain.UserIDFromContext(r.Context()) != "user123" {
			t.Errorf("user_id mismatch: expected user123, got %s", domain.UserIDFromContext(r.Context()))
		}
		if domain.AppIDFromContext(r.Context()) != "tenant-abc" {
			t.Errorf("app_id mismatch: expected tenant-abc, got %s", domain.AppIDFromContext(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("next handler not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingAuthHeader(t *testing.T) {
	middleware := AuthMiddleware(testSecret)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidBearerFormat(t *testing.T) {
	middleware := AuthMiddleware(testSecret)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Not Bearer
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidSignature(t *testing.T) {
	token := validToken("user123", "tenant-abc")
	middleware := AuthMiddleware("wrong-secret")

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingUserIDClaim(t *testing.T) {
	claims := Claims{
		Sub: "", // Missing
		Tid: "tenant-abc",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(testSecret))

	middleware := AuthMiddleware(testSecret)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingTenantIDClaim(t *testing.T) {
	claims := Claims{
		Sub: "user123",
		Tid: "", // Missing
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(testSecret))

	middleware := AuthMiddleware(testSecret)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_TenantIsolation(t *testing.T) {
	// Test that context correctly isolates tenants
	token1 := validToken("user1", "tenant-a")
	token2 := validToken("user2", "tenant-b")

	middleware := AuthMiddleware(testSecret)

	testCases := []struct {
		name      string
		token     string
		wantUser  string
		wantAppID string
	}{
		{"tenant-a", token1, "user1", "tenant-a"},
		{"tenant-b", token2, "user2", "tenant-b"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user := domain.UserIDFromContext(r.Context())
				appID := domain.AppIDFromContext(r.Context())

				if user != tc.wantUser {
					t.Errorf("user_id: expected %q, got %q", tc.wantUser, user)
				}
				if appID != tc.wantAppID {
					t.Errorf("app_id: expected %q, got %q", tc.wantAppID, appID)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		})
	}
}

func BenchmarkAuthMiddleware_ValidToken(b *testing.B) {
	token := validToken("user123", "tenant-abc")
	mw := AuthMiddleware(testSecret)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest("GET", "/bench", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkAuthMiddleware_InvalidToken(b *testing.B) {
	mw := AuthMiddleware(testSecret)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	b.ResetTimer()
	for range b.N {
		req := httptest.NewRequest("GET", "/bench", nil)
		req.Header.Set("Authorization", "Bearer invalid.token.here")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	claims := Claims{
		Sub: "user123",
		Tid: "tenant-abc",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(testSecret))

	middleware := AuthMiddleware(testSecret)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
