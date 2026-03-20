// Package api implements the REST + WebSocket API server for external
// orchestrators (Claws) to connect to the Axiom engine.
//
// See Architecture.md Section 24 for the full specification.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethan03805/axiom/internal/events"
)

// ServerConfig holds API server configuration.
type ServerConfig struct {
	Port            int
	RateLimitRPM    int
	AllowedIPs      []string
	TokenStorageDir string // Path to persistent token storage (e.g. ~/.axiom/api-tokens/)
}

// Server provides the REST + WebSocket API for Axiom.
// See Architecture Section 24.2.
type Server struct {
	config   ServerConfig
	emitter  *events.Emitter
	auth     *TokenAuth
	limiter  *RateLimiter
	wsHub    *WSHub
	httpSrv  *http.Server
	handlers *Handlers
}

// NewServer creates an API server. If TokenStorageDir is set, persisted tokens
// are loaded from disk so that tokens generated via `axiom api token generate`
// are recognized by the running server. See Architecture Section 24.3.
func NewServer(config ServerConfig, emitter *events.Emitter) (*Server, error) {
	var auth *TokenAuth
	if config.TokenStorageDir != "" {
		var err error
		auth, err = NewTokenAuthWithStorage(config.TokenStorageDir)
		if err != nil {
			return nil, fmt.Errorf("init token auth with storage: %w", err)
		}
	} else {
		auth = NewTokenAuth()
	}
	limiter := NewRateLimiter(config.RateLimitRPM)
	wsHub := NewWSHub(emitter)

	s := &Server{
		config:  config,
		emitter: emitter,
		auth:    auth,
		limiter: limiter,
		wsHub:   wsHub,
	}
	s.handlers = &Handlers{emitter: emitter}
	return s, nil
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Wrap with middleware stack: audit -> IP restriction -> auth -> rate limit.
	handler := s.auditMiddleware(
		s.ipRestrictionMiddleware(
			s.authMiddleware(
				s.rateLimitMiddleware(mux),
			),
		),
	)

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start WebSocket hub.
	go s.wsHub.Run()

	go s.httpSrv.ListenAndServe()

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.wsHub.Stop()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// Auth returns the token authentication manager for external use (e.g. CLI).
func (s *Server) Auth() *TokenAuth {
	return s.auth
}

// Handlers returns the handler struct for external wiring (e.g. WireHandlersToCoordinator).
func (s *Server) Handlers() *Handlers {
	return s.handlers
}

// registerRoutes sets up all REST endpoints per Architecture Section 24.2.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	h := s.handlers

	// Project lifecycle.
	mux.HandleFunc("POST /api/v1/projects", h.CreateProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/run", h.RunProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/srs/approve", h.ApproveSRS)
	mux.HandleFunc("POST /api/v1/projects/{id}/srs/reject", h.RejectSRS)
	mux.HandleFunc("POST /api/v1/projects/{id}/eco/approve", h.ApproveECO)
	mux.HandleFunc("POST /api/v1/projects/{id}/eco/reject", h.RejectECO)
	mux.HandleFunc("POST /api/v1/projects/{id}/pause", h.PauseProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/resume", h.ResumeProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/cancel", h.CancelProject)

	// Data queries.
	mux.HandleFunc("GET /api/v1/projects/{id}/status", h.GetStatus)
	mux.HandleFunc("GET /api/v1/projects/{id}/tasks", h.GetTasks)
	mux.HandleFunc("GET /api/v1/projects/{id}/tasks/{tid}/attempts", h.GetAttempts)
	mux.HandleFunc("GET /api/v1/projects/{id}/costs", h.GetCosts)
	mux.HandleFunc("GET /api/v1/projects/{id}/events", h.GetEvents)
	mux.HandleFunc("GET /api/v1/models", h.GetModels)

	// Semantic index.
	mux.HandleFunc("POST /api/v1/index/query", h.QueryIndex)

	// WebSocket.
	mux.HandleFunc("/ws/projects/{id}", s.wsHub.HandleWebSocket)
}

// --- Middleware ---

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for WebSocket upgrades (auth checked in WS handler).
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing authorization token"})
			return
		}

		tokenInfo, err := s.auth.Validate(token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}

		// Scope check: read-only tokens can only use GET.
		if tokenInfo.Scope == ScopeReadOnly && r.Method != http.MethodGet {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "read-only token cannot perform this action"})
			return
		}

		// Store token ID in context for audit logging.
		ctx := context.WithValue(r.Context(), contextKeyTokenID, tokenInfo.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token != "" && !s.limiter.Allow(token) {
			retryAfter := s.limiter.SecondsUntilReset(token)
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ipRestrictionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.config.AllowedIPs) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := extractClientIP(r)
		for _, allowed := range s.config.AllowedIPs {
			if strings.Contains(allowed, "/") {
				_, cidr, err := net.ParseCIDR(allowed)
				if err == nil && cidr.Contains(net.ParseIP(clientIP)) {
					next.ServeHTTP(w, r)
					return
				}
			} else if clientIP == allowed {
				next.ServeHTTP(w, r)
				return
			}
		}

		writeJSON(w, http.StatusForbidden, map[string]string{"error": "IP not allowed"})
	})
}

func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		tokenID, _ := r.Context().Value(contextKeyTokenID).(string)
		s.emitter.Emit(events.Event{
			Type:      events.EventTaskCreated, // Generic event for API audit
			AgentType: "api",
			Details: map[string]interface{}{
				"method":     r.Method,
				"path":       r.URL.Path,
				"status":     rec.statusCode,
				"token_id":   tokenID,
				"client_ip":  extractClientIP(r),
				"latency_ms": time.Since(start).Milliseconds(),
			},
		})
	})
}

type contextKey string

const contextKeyTokenID contextKey = "token_id"

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func extractClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
