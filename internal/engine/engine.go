package engine

import (
	"context"
)

// Engine is the core orchestration engine that manages the lifecycle
// of tasks, agents, and the overall execution pipeline.
type Engine struct {
	config *Config
	// db     *state.Store  // will be wired in later phases
	// orch   *orchestrator.Orchestrator
}

// New creates a new Engine instance with the given configuration.
func New(cfg *Config) (*Engine, error) {
	return &Engine{
		config: cfg,
	}, nil
}

// Start begins the engine's main loop, processing tasks and managing agents.
func (e *Engine) Start(ctx context.Context) error {
	return nil
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop(ctx context.Context) error {
	return nil
}

// Status returns the current status of the engine.
func (e *Engine) Status() string {
	return "idle"
}
