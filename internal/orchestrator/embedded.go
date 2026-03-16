// Package orchestrator implements the embedded orchestrator runtime that
// manages orchestrator containers (Claude Code, Codex, OpenCode) with
// full lifecycle management, IPC action handling, and bootstrap mode.
//
// See Architecture.md Section 8 for the Orchestrator specification.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/container"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

// Phase represents the orchestrator's current operating phase.
type Phase string

const (
	// PhaseBootstrap is the SRS generation phase with scoped context.
	// See Architecture Section 8.7.
	PhaseBootstrap Phase = "bootstrap"

	// PhaseExecution is the normal TaskSpec-building and execution phase.
	PhaseExecution Phase = "execution"

	// PhasePaused means execution is paused by the user.
	PhasePaused Phase = "paused"

	// PhaseCompleted means all tasks are done.
	PhaseCompleted Phase = "completed"
)

// Runtime identifies the orchestrator software being used.
// See Architecture Section 8.3.
type Runtime string

const (
	RuntimeClaudeCode Runtime = "claude-code"
	RuntimeCodex      Runtime = "codex"
	RuntimeOpenCode   Runtime = "opencode"
	RuntimeClaw       Runtime = "claw"
)

// EmbeddedConfig holds configuration for the embedded orchestrator.
type EmbeddedConfig struct {
	Runtime     Runtime
	Image       string // Docker image for the orchestrator container
	CPULimit    float64
	MemoryLimit string
	TimeoutMin  int
	ProjectSlug string
	BudgetUSD   float64
}

// Embedded manages the lifecycle of an embedded orchestrator container.
// In embedded mode, all inference goes through the engine's Inference Broker.
// See Architecture Section 8.2 (Embedded Mode).
type Embedded struct {
	config  EmbeddedConfig
	ctrMgr  *container.Manager
	db      *state.DB
	emitter *events.Emitter
	writer  *ipc.Writer

	mu            sync.Mutex
	phase         Phase
	containerName string
	containerID   string
	projectID     string
}

// NewEmbedded creates an embedded orchestrator runtime manager.
func NewEmbedded(
	config EmbeddedConfig,
	ctrMgr *container.Manager,
	db *state.DB,
	emitter *events.Emitter,
	writer *ipc.Writer,
) *Embedded {
	return &Embedded{
		config:  config,
		ctrMgr:  ctrMgr,
		db:      db,
		emitter: emitter,
		writer:  writer,
		phase:   PhaseBootstrap,
	}
}

// Start spawns the orchestrator container and delivers the initial prompt.
// In bootstrap mode, context is scoped per Architecture Section 8.7.
func (e *Embedded) Start(ctx context.Context, projectID, prompt string, isGreenfield bool) error {
	e.mu.Lock()
	e.projectID = projectID
	e.mu.Unlock()

	// Spawn the orchestrator container.
	result, err := e.ctrMgr.SpawnMeeseeks(ctx, container.SpawnRequest{
		TaskID:      "orchestrator-" + projectID,
		Image:       e.config.Image,
		CPULimit:    e.config.CPULimit,
		MemoryLimit: e.config.MemoryLimit,
		TimeoutMin:  e.config.TimeoutMin,
	})
	if err != nil {
		return fmt.Errorf("spawn orchestrator: %w", err)
	}

	e.mu.Lock()
	e.containerName = result.ContainerName
	e.containerID = result.ContainerID
	e.mu.Unlock()

	// Build and deliver the bootstrap context.
	bootstrapCtx := e.buildBootstrapContext(prompt, isGreenfield)

	// Send the bootstrap context to the orchestrator via IPC.
	taskID := "orchestrator-" + projectID
	msg := &ipc.ActionRequestMessage{
		Header: ipc.Header{Type: ipc.TypeActionRequest, TaskID: taskID},
		Action: "bootstrap",
		Parameters: mustJSON(map[string]interface{}{
			"prompt":       prompt,
			"project_id":   projectID,
			"runtime":      string(e.config.Runtime),
			"budget_usd":   e.config.BudgetUSD,
			"is_greenfield": isGreenfield,
			"context":      bootstrapCtx,
		}),
	}
	if err := e.writer.Send(taskID, msg); err != nil {
		return fmt.Errorf("send bootstrap context: %w", err)
	}

	e.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		AgentType: "orchestrator",
		AgentID:   result.ContainerName,
		Details: map[string]interface{}{
			"runtime":      string(e.config.Runtime),
			"phase":        string(PhaseBootstrap),
			"is_greenfield": isGreenfield,
		},
	})

	return nil
}

// TransitionToExecution switches from bootstrap mode to execution mode.
// Called after SRS approval. See Architecture Section 8.7.
func (e *Embedded) TransitionToExecution() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.phase = PhaseExecution
}

// Pause stops new task spawning but lets running containers complete.
func (e *Embedded) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.phase = PhasePaused
}

// Resume resumes execution after a pause.
func (e *Embedded) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.phase == PhasePaused {
		e.phase = PhaseExecution
	}
}

// Complete marks the orchestrator as completed.
func (e *Embedded) Complete() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.phase = PhaseCompleted
}

// Phase returns the current orchestrator phase.
func (e *Embedded) CurrentPhase() Phase {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.phase
}

// Stop destroys the orchestrator container.
func (e *Embedded) Stop(ctx context.Context) error {
	e.mu.Lock()
	name := e.containerName
	e.mu.Unlock()

	if name == "" {
		return nil
	}

	// Send shutdown message first for graceful termination.
	taskID := "orchestrator-" + e.projectID
	_ = e.writer.Send(taskID, &ipc.ShutdownMessage{
		Header: ipc.Header{Type: ipc.TypeShutdown, TaskID: taskID},
		Reason: "engine shutdown",
	})

	// Give it a moment to process, then destroy.
	time.Sleep(500 * time.Millisecond)
	return e.ctrMgr.Destroy(ctx, name)
}

// ContainerName returns the orchestrator container name.
func (e *Embedded) ContainerName() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.containerName
}

// buildBootstrapContext builds the initial context for the orchestrator.
// See Architecture Section 8.7.
func (e *Embedded) buildBootstrapContext(prompt string, isGreenfield bool) map[string]interface{} {
	ctx := map[string]interface{}{
		"prompt":  prompt,
		"mode":    "bootstrap",
		"runtime": string(e.config.Runtime),
	}

	if isGreenfield {
		// Greenfield: only prompt + project config. No repo-map or index.
		ctx["project_type"] = "greenfield"
	} else {
		// Existing project: read-only repo-map + semantic index access.
		ctx["project_type"] = "existing"
		ctx["index_access"] = true
	}

	return ctx
}

// mustJSON marshals a value to json.RawMessage, panicking on error.
func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return data
}
