package api

import (
	"context"
	"fmt"
	"net/http"
)

// Server provides the REST API for Axiom.
type Server struct {
	port         int
	rateLimitRPM int
	allowedIPs   []string
	server       *http.Server
}

// New creates a new API Server.
func New(port, rateLimitRPM int, allowedIPs []string) *Server {
	return &Server{
		port:         port,
		rateLimitRPM: rateLimitRPM,
		allowedIPs:   allowedIPs,
	}
}

// Start begins listening for HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	return nil
}

// Stop gracefully stops the API server.
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/tasks", s.handleTasks)
	mux.HandleFunc("/api/v1/budget", s.handleBudget)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBudget(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
