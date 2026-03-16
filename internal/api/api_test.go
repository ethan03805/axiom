package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	emitter := events.NewEmitter()
	srv := NewServer(ServerConfig{Port: 0, RateLimitRPM: 120}, emitter)
	return srv
}

// --- Token Auth tests ---

func TestTokenGenerate(t *testing.T) {
	auth := NewTokenAuth()

	info, token := auth.Generate(ScopeFullControl, 24*time.Hour)
	if !strings.HasPrefix(token, "axm_sk_") {
		t.Errorf("token should start with axm_sk_, got: %s", token[:10])
	}
	if info.ID == "" {
		t.Error("expected non-empty token ID")
	}
	if info.Scope != ScopeFullControl {
		t.Errorf("scope = %s, want full-control", info.Scope)
	}
}

func TestTokenValidate(t *testing.T) {
	auth := NewTokenAuth()
	_, token := auth.Generate(ScopeFullControl, 1*time.Hour)

	info, err := auth.Validate(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if info.Scope != ScopeFullControl {
		t.Errorf("scope = %s", info.Scope)
	}
}

func TestTokenValidateInvalid(t *testing.T) {
	auth := NewTokenAuth()
	_, err := auth.Validate("axm_sk_nonexistent")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestTokenExpiration(t *testing.T) {
	auth := NewTokenAuth()
	_, token := auth.Generate(ScopeFullControl, 1*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	_, err := auth.Validate(token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestTokenRevocation(t *testing.T) {
	auth := NewTokenAuth()
	info, token := auth.Generate(ScopeFullControl, 1*time.Hour)

	if err := auth.Revoke(info.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err := auth.Validate(token)
	if err == nil {
		t.Error("expected error for revoked token")
	}
}

func TestTokenList(t *testing.T) {
	auth := NewTokenAuth()
	auth.Generate(ScopeFullControl, 1*time.Hour)
	auth.Generate(ScopeReadOnly, 1*time.Hour)

	list := auth.List()
	if len(list) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(list))
	}
}

func TestTokenRevokeNotInList(t *testing.T) {
	auth := NewTokenAuth()
	info, _ := auth.Generate(ScopeFullControl, 1*time.Hour)
	auth.Generate(ScopeReadOnly, 1*time.Hour)

	auth.Revoke(info.ID)

	list := auth.List()
	if len(list) != 1 {
		t.Errorf("expected 1 token after revocation, got %d", len(list))
	}
}

// --- Rate Limiter tests ---

func TestRateLimiterAllows(t *testing.T) {
	rl := NewRateLimiter(5) // 5 per minute

	for i := 0; i < 5; i++ {
		if !rl.Allow("token-1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("token-1") {
		t.Error("6th request should be rate limited")
	}
}

func TestRateLimiterPerToken(t *testing.T) {
	rl := NewRateLimiter(2)

	rl.Allow("token-a")
	rl.Allow("token-a")

	// Token A is at limit, but token B should still be allowed.
	if !rl.Allow("token-b") {
		t.Error("token-b should not be affected by token-a's limit")
	}
}

func TestRateLimiterRemainingRequests(t *testing.T) {
	rl := NewRateLimiter(10)

	rl.Allow("t1")
	rl.Allow("t1")
	rl.Allow("t1")

	remaining := rl.RequestsRemaining("t1")
	if remaining != 7 {
		t.Errorf("expected 7 remaining, got %d", remaining)
	}

	// New token should have full quota.
	remaining = rl.RequestsRemaining("t-new")
	if remaining != 10 {
		t.Errorf("expected 10 remaining for new token, got %d", remaining)
	}
}

// --- Handler tests ---

func TestHandlerGetStatus(t *testing.T) {
	srv := setupTestServer(t)

	// Create a request to the status endpoint.
	req := httptest.NewRequest("GET", "/api/v1/projects/proj-1/status", nil)
	w := httptest.NewRecorder()

	srv.handlers.GetStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "unknown" {
		t.Errorf("status = %s", resp["status"])
	}
}

func TestHandlerCreateProject(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"prompt": "Build me a REST API", "budget": 10.0}`
	req := httptest.NewRequest("POST", "/api/v1/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handlers.CreateProject(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

func TestHandlerQueryIndex(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"query_type": "lookup_symbol", "params": {"name": "HandleAuth"}}`
	req := httptest.NewRequest("POST", "/api/v1/index/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handlers.QueryIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandlerQueryIndexBadJSON(t *testing.T) {
	srv := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/v1/index/query", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	srv.handlers.QueryIndex(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- WebSocket Hub tests ---

func TestWSHubClientCount(t *testing.T) {
	emitter := events.NewEmitter()
	hub := NewWSHub(emitter)

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

// --- Integration: auth middleware ---

func TestAuthMiddlewareRejectsNoToken(t *testing.T) {
	srv := setupTestServer(t)

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/projects/1/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMiddlewareAcceptsValidToken(t *testing.T) {
	srv := setupTestServer(t)
	_, token := srv.auth.Generate(ScopeFullControl, 1*time.Hour)

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/projects/1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthMiddlewareReadOnlyBlocksPost(t *testing.T) {
	srv := setupTestServer(t)
	_, token := srv.auth.Generate(ScopeReadOnly, 1*time.Hour)

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	srv := setupTestServer(t)
	srv.limiter = NewRateLimiter(2)
	_, token := srv.auth.Generate(ScopeFullControl, 1*time.Hour)

	handler := srv.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if i < 2 && w.Code != http.StatusOK {
			t.Errorf("request %d: status = %d, want 200", i+1, w.Code)
		}
		if i == 2 && w.Code != http.StatusTooManyRequests {
			t.Errorf("request %d: status = %d, want 429", i+1, w.Code)
		}
	}
}
