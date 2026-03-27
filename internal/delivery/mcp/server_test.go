package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obrel/nexus/config"
)

func TestMCPHealthReadiness(t *testing.T) {
	server := New(&config.MCPConfig{Port: 0})
	server.RegisterRoutes()

	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	w1 := httptest.NewRecorder()
	server.Router().ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected /health status 200, got %d", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w2 := httptest.NewRecorder()
	server.Router().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected /ready status 200, got %d", w2.Code)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/.well-known/mcp", nil)
	w3 := httptest.NewRecorder()
	server.Router().ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected /.well-known/mcp status 200, got %d", w3.Code)
	}
}
