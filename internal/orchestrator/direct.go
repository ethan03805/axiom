// Package orchestrator implements orchestrator runtime management.
//
// DirectOrchestrator provides an in-process orchestrator that calls the inference
// broker directly (via OpenRouter or BitNet) without requiring Docker containers.
// This is the fallback when Docker is unavailable or when the configured runtime
// is "claw" and no external Claw is connected.
//
// The DirectOrchestrator follows the same lifecycle as the Embedded orchestrator
// (Architecture Section 8) but runs in the engine process rather than a container.
// All inference is still routed through the broker for budget tracking and audit.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/srs"
	"github.com/ethan03805/axiom/internal/state"
)

// DirectConfig holds configuration for the direct (in-process) orchestrator.
type DirectConfig struct {
	Runtime              Runtime
	BudgetUSD            float64
	ProjectSlug          string
	Model                string // Default model to use for orchestration (e.g. "anthropic/claude-sonnet-4")
	SRSApprovalDelegate  string // "user" (default) or "claw" -- controls whether SRS is auto-approved
}

// Direct is an in-process orchestrator that calls the inference broker directly
// without requiring Docker containers. It generates an SRS from the user prompt,
// decomposes it into tasks, and creates them in the database for the execution
// loop to process.
type Direct struct {
	config      DirectConfig
	infBroker   *broker.Broker
	db          *state.DB
	emitter     *events.Emitter
	srsApproval *srs.ApprovalManager

	mu        sync.Mutex
	phase     Phase
	projectID string
	prompt    string
}

// NewDirect creates a direct orchestrator that runs in the engine process.
func NewDirect(
	config DirectConfig,
	infBroker *broker.Broker,
	db *state.DB,
	emitter *events.Emitter,
	srsApproval *srs.ApprovalManager,
) *Direct {
	if config.Model == "" {
		config.Model = "anthropic/claude-sonnet-4"
	}
	return &Direct{
		config:      config,
		infBroker:   infBroker,
		db:          db,
		emitter:     emitter,
		srsApproval: srsApproval,
		phase:       PhaseBootstrap,
	}
}

// Start begins the orchestration process: generates an SRS from the prompt,
// then decomposes it into tasks. Runs asynchronously in a goroutine.
func (d *Direct) Start(ctx context.Context, projectID, prompt string, isGreenfield bool) error {
	d.mu.Lock()
	d.projectID = projectID
	d.prompt = prompt
	d.mu.Unlock()

	d.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		AgentType: "orchestrator",
		AgentID:   "direct-" + projectID,
		Details: map[string]interface{}{
			"runtime":       string(d.config.Runtime),
			"mode":          "direct",
			"phase":         string(PhaseBootstrap),
			"is_greenfield": isGreenfield,
		},
	})

	go d.run(ctx, projectID, prompt, isGreenfield)
	return nil
}

// run executes the orchestration pipeline: SRS generation -> task decomposition.
func (d *Direct) run(ctx context.Context, projectID, prompt string, isGreenfield bool) {
	// Step 1: Generate SRS via inference broker.
	srsPrompt := buildSRSPrompt(prompt, isGreenfield)

	d.emitter.Emit(events.Event{
		Type:      events.EventSRSSubmitted,
		AgentType: "orchestrator",
		AgentID:   "direct-" + projectID,
		Details: map[string]interface{}{
			"status": "generating_srs",
		},
	})

	srsResp, err := d.infBroker.RouteRequest(ctx, &broker.InferenceRequest{
		TaskID:    "orchestrator-" + projectID,
		ModelID:   d.config.Model,
		AgentType: "orchestrator",
		Messages: []broker.ChatMessage{
			{Role: "system", Content: "You are a software architect generating a Software Requirements Specification (SRS) for the Axiom platform. Follow the SRS format exactly."},
			{Role: "user", Content: srsPrompt},
		},
		MaxTokens:   16384,
		Temperature: 0.3,
	})
	if err != nil {
		d.emitter.Emit(events.Event{
			Type:      events.EventTaskFailed,
			AgentType: "orchestrator",
			AgentID:   "direct-" + projectID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("SRS generation failed: %v", err),
				"phase": "srs_generation",
			},
		})
		return
	}

	// Store the SRS content and submit for approval.
	// See Architecture Section 5.1 steps 3-5 and Section 6.2.
	srsContent := srsResp.Content
	d.emitter.Emit(events.Event{
		Type:      events.EventSRSSubmitted,
		AgentType: "orchestrator",
		AgentID:   "direct-" + projectID,
		Details: map[string]interface{}{
			"status":     "srs_generated",
			"srs_length": len(srsContent),
		},
	})

	// Submit the SRS draft for approval. This writes .axiom/srs.md and
	// validates the SRS format per Architecture Section 6.1.
	if d.srsApproval != nil {
		validationErrs, err := d.srsApproval.SubmitDraft(srsContent)
		if err != nil {
			d.emitter.Emit(events.Event{
				Type:      events.EventTaskFailed,
				AgentType: "orchestrator",
				AgentID:   "direct-" + projectID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("submit SRS draft: %v", err),
					"phase": "srs_approval",
				},
			})
			return
		}
		if len(validationErrs) > 0 {
			d.emitter.Emit(events.Event{
				Type:      events.EventSRSSubmitted,
				AgentType: "orchestrator",
				AgentID:   "direct-" + projectID,
				Details: map[string]interface{}{
					"status":           "validation_warnings",
					"validation_errors": validationErrs,
				},
			})
			// Continue despite validation warnings -- the SRS is still usable.
		}

		// Only auto-approve the SRS when the delegate is set to "claw"
		// (meaning a Claw proxy is trusted to approve). When the delegate
		// is "user" (default), wait for the user to approve via CLI/GUI.
		// See Architecture Section 8.5.
		if d.config.SRSApprovalDelegate == "claw" {
			hash, err := d.srsApproval.Approve("direct-orchestrator")
			if err != nil {
				d.emitter.Emit(events.Event{
					Type:      events.EventTaskFailed,
					AgentType: "orchestrator",
					AgentID:   "direct-" + projectID,
					Details: map[string]interface{}{
						"error": fmt.Sprintf("approve SRS: %v", err),
						"phase": "srs_approval",
					},
				})
				return
			}
			d.emitter.Emit(events.Event{
				Type:      events.EventSRSApproved,
				AgentType: "orchestrator",
				AgentID:   "direct-" + projectID,
				Details: map[string]interface{}{
					"sha256":      hash,
					"approved_by": "direct-orchestrator",
				},
			})
		} else {
			// Wait for user approval. Poll the SRS approval state.
			d.emitter.Emit(events.Event{
				Type:      events.EventSRSSubmitted,
				AgentType: "orchestrator",
				AgentID:   "direct-" + projectID,
				Details: map[string]interface{}{
					"status":   "awaiting_user_approval",
					"delegate": d.config.SRSApprovalDelegate,
				},
			})
			fmt.Println("\nSRS generated and written to .axiom/srs.md")
			fmt.Println("Review the SRS, then approve with: axiom srs approve")
			fmt.Println("Or reject with: axiom srs reject \"<feedback>\"")

			// Poll until the SRS is approved or the context is cancelled.
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			approved := false
			for !approved {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if d.srsApproval.IsApproved() {
						approved = true
					}
				}
			}
		}
	}

	// Step 2: Decompose SRS into tasks.
	d.mu.Lock()
	d.phase = PhaseExecution
	d.mu.Unlock()

	decomposePrompt := buildDecomposePrompt(srsContent)

	decompResp, err := d.infBroker.RouteRequest(ctx, &broker.InferenceRequest{
		TaskID:    "orchestrator-" + projectID,
		ModelID:   d.config.Model,
		AgentType: "orchestrator",
		Messages: []broker.ChatMessage{
			{Role: "system", Content: "You are a task decomposition engine. Given an SRS, break it into atomic tasks. Output valid JSON only."},
			{Role: "user", Content: decomposePrompt},
		},
		MaxTokens:   8192,
		Temperature: 0.2,
	})
	if err != nil {
		d.emitter.Emit(events.Event{
			Type:      events.EventTaskFailed,
			AgentType: "orchestrator",
			AgentID:   "direct-" + projectID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("task decomposition failed: %v", err),
				"phase": "task_decomposition",
			},
		})
		return
	}

	// Parse and create tasks from the decomposition response.
	decomp := parseTaskDecomposition(decompResp.Content, projectID)

	var tasks []*state.Task
	var metadata []TaskMetadata
	if decomp != nil {
		tasks = decomp.Tasks
		metadata = decomp.Metadata
	}

	if len(tasks) == 0 {
		// Create a single fallback task from the prompt.
		tasks = []*state.Task{{
			ID:          fmt.Sprintf("%s-task-001", projectID),
			Title:       "Implement project from SRS",
			Description: prompt,
			Status:      string(state.TaskStatusQueued),
			Tier:        "standard",
			TaskType:    "implementation",
		}}
		metadata = nil
	}

	// Persist tasks to the database.
	if err := d.db.CreateTaskBatch(tasks); err != nil {
		d.emitter.Emit(events.Event{
			Type:      events.EventTaskFailed,
			AgentType: "orchestrator",
			AgentID:   "direct-" + projectID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("task creation failed: %v", err),
			},
		})
		return
	}

	// Persist task metadata: SRS refs, dependencies, and target files.
	// These populate the junction tables required by Architecture Section 15.2.
	for _, md := range metadata {
		for _, ref := range md.SRSRefs {
			_ = d.db.AddTaskSRSRef(md.TaskID, ref)
		}
		for _, dep := range md.Dependencies {
			if err := d.db.AddTaskDependency(md.TaskID, dep); err != nil {
				d.emitter.Emit(events.Event{
					Type:      events.EventTaskFailed,
					AgentType: "orchestrator",
					AgentID:   "direct-" + projectID,
					Details: map[string]interface{}{
						"warning": fmt.Sprintf("add dependency %s->%s: %v", md.TaskID, dep, err),
					},
				})
			}
		}
		for _, f := range md.TargetFiles {
			_ = d.db.AddTaskTargetFile(md.TaskID, f, "file")
		}
	}

	d.emitter.Emit(events.Event{
		Type:      events.EventTaskCreated,
		AgentType: "orchestrator",
		AgentID:   "direct-" + projectID,
		Details: map[string]interface{}{
			"task_count": len(tasks),
			"status":     "tasks_created",
		},
	})
}

// Pause pauses the orchestrator.
func (d *Direct) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.phase = PhasePaused
}

// Resume resumes the orchestrator.
func (d *Direct) Resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase == PhasePaused {
		d.phase = PhaseExecution
	}
}

// Complete marks the orchestrator as completed.
func (d *Direct) Complete() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.phase = PhaseCompleted
}

// Stop is a no-op for the direct orchestrator (no container to destroy).
func (d *Direct) Stop(_ context.Context) error {
	return nil
}

// CurrentPhase returns the current phase.
func (d *Direct) CurrentPhase() Phase {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phase
}

// buildSRSPrompt creates the prompt for SRS generation.
func buildSRSPrompt(userPrompt string, isGreenfield bool) string {
	projectType := "existing project"
	if isGreenfield {
		projectType = "greenfield project"
	}

	return fmt.Sprintf(`Generate a Software Requirements Specification (SRS) for the following %s:

USER PROMPT:
%s

The SRS must follow this exact structure:

# SRS: <Project Name>

## 1. Architecture
### 1.1 System Overview
### 1.2 Component Breakdown
### 1.3 Technology Decisions
### 1.4 Data Model
### 1.5 Directory Structure

## 2. Requirements & Constraints
### 2.1 Functional Requirements (FR-001, FR-002, ...)
### 2.2 Non-Functional Requirements (NFR-001, ...)
### 2.3 Constraints
### 2.4 Assumptions

## 3. Test Strategy
### 3.1 Unit Testing
### 3.2 Integration Testing

## 4. Acceptance Criteria
### 4.1 Per-Component Criteria (AC-001, AC-002, ...)
### 4.2 Integration Criteria (IC-001, ...)
### 4.3 Completion Definition

Be thorough and specific. Every requirement must be testable.`, projectType, userPrompt)
}

// buildDecomposePrompt creates the prompt for task decomposition.
func buildDecomposePrompt(srsContent string) string {
	return fmt.Sprintf(`Given the following SRS, decompose it into atomic implementation tasks.

SRS:
%s

Output a JSON array of task objects. Each task has:
- "id": unique identifier like "task-001"
- "title": short descriptive title
- "description": what needs to be implemented
- "tier": "local" | "cheap" | "standard" | "premium"
- "task_type": "implementation" | "test"
- "srs_refs": array of requirement IDs this task implements (e.g. ["FR-001", "AC-001"])
- "dependencies": array of task IDs this task depends on
- "target_files": array of file paths this task will create/modify

Output ONLY the JSON array, no other text.`, srsContent)
}

// TaskMetadata holds the parsed junction-table data for a single task.
// These are persisted separately from the task record itself.
type TaskMetadata struct {
	TaskID       string
	SRSRefs      []string
	Dependencies []string
	TargetFiles  []string
}

// DecompositionResult holds the parsed tasks and their associated metadata.
type DecompositionResult struct {
	Tasks    []*state.Task
	Metadata []TaskMetadata
}

// parseTaskDecomposition parses the LLM's task decomposition response into
// state.Task objects and their associated metadata (SRS refs, dependencies,
// target files). See Architecture Section 15.1-15.5.
func parseTaskDecomposition(response, projectID string) *DecompositionResult {
	// Try to parse as JSON array of tasks.
	type rawTask struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Tier         string   `json:"tier"`
		TaskType     string   `json:"task_type"`
		SRSRefs      []string `json:"srs_refs"`
		Dependencies []string `json:"dependencies"`
		TargetFiles  []string `json:"target_files"`
	}

	// Find JSON array in the response (LLM may include surrounding text).
	start := -1
	end := -1
	depth := 0
	for i, c := range response {
		if c == '[' && start == -1 {
			start = i
			depth = 1
		} else if c == '[' && start != -1 {
			depth++
		} else if c == ']' && start != -1 {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}

	if start == -1 || end == -1 {
		return nil
	}

	jsonStr := response[start:end]
	var rawTasks []rawTask

	// Use json.Unmarshal -- import is available via the package.
	if err := jsonUnmarshalTasks([]byte(jsonStr), &rawTasks); err != nil {
		return nil
	}

	result := &DecompositionResult{
		Tasks:    make([]*state.Task, 0, len(rawTasks)),
		Metadata: make([]TaskMetadata, 0, len(rawTasks)),
	}
	now := time.Now()
	for _, rt := range rawTasks {
		taskID := rt.ID
		if taskID == "" {
			taskID = fmt.Sprintf("%s-task-%03d", projectID, len(result.Tasks)+1)
		}
		tier := rt.Tier
		if tier == "" {
			tier = "standard"
		}
		taskType := rt.TaskType
		if taskType == "" {
			taskType = "implementation"
		}

		result.Tasks = append(result.Tasks, &state.Task{
			ID:          taskID,
			Title:       rt.Title,
			Description: rt.Description,
			Status:      string(state.TaskStatusQueued),
			Tier:        tier,
			TaskType:    taskType,
			CreatedAt:   now,
		})
		result.Metadata = append(result.Metadata, TaskMetadata{
			TaskID:       taskID,
			SRSRefs:      rt.SRSRefs,
			Dependencies: rt.Dependencies,
			TargetFiles:  rt.TargetFiles,
		})
	}

	return result
}

// jsonUnmarshalTasks parses a JSON byte slice into the given value.
func jsonUnmarshalTasks(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
