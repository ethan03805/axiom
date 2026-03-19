// Package api -- wiring.go connects API handler callbacks to engine operations.
//
// The api package cannot import the engine package directly (circular dependency).
// Instead, we define a CoordinatorAPI interface that the engine.Coordinator satisfies.
// The WireHandlersToCoordinator function sets all OnXxx callbacks on Handlers
// to call the corresponding CoordinatorAPI methods.
//
// See Architecture Section 24.2 and BUILD_PLAN Phase 16.
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ethan03805/axiom/internal/budget"
	"github.com/ethan03805/axiom/internal/state"
)

// CoordinatorAPI is the interface that the engine.Coordinator must satisfy
// for API handler wiring. This avoids a circular dependency between the api
// and engine packages.
type CoordinatorAPI interface {
	// DB returns the state database for task/event queries.
	DB() *state.DB

	// BudgetTracker returns the budget tracker for cost reports.
	BudgetTracker() *budget.Tracker

	// SRS operations.
	ApproveSRS(approvedBy string) (string, error)
	RejectSRS(feedback string) error

	// ECO operations.
	ApproveECO(ecoID int64, approvedBy string) error
	RejectECO(ecoID int64, rejectedBy string) error

	// Lifecycle operations.
	Pause()
	Resume()
	Cancel(ctx context.Context) error

	// CompletionPercentage returns the percent of tasks that are done.
	CompletionPercentage() float64

	// IsPaused returns whether the coordinator is currently paused.
	IsPaused() bool

	// ActiveContainerCount returns the number of running agent containers.
	ActiveContainerCount() int
}

// WireHandlersToCoordinator connects all API handler callbacks to the
// corresponding coordinator operations. This is called during server
// initialization to bind the REST API to the engine.
//
// See Architecture Section 24.2 for the full endpoint mapping.
func WireHandlersToCoordinator(handlers *Handlers, coord CoordinatorAPI) {
	// OnCreateProject: create a project record in the database.
	// For now this is a stub that returns a generated project ID.
	handlers.OnCreateProject = func(prompt string, budgetUSD float64) (string, error) {
		// TODO: full project creation logic (init .axiom dir, config, etc.)
		return fmt.Sprintf("proj-%d", time.Now().UnixMilli()), nil
	}

	// OnRunProject: start project execution.
	// Stub for now -- full implementation requires orchestrator lifecycle.
	handlers.OnRunProject = func(projectID, prompt string) error {
		// TODO: wire to coordinator.Start() / orchestrator launch
		return nil
	}

	// OnApproveSRS: approve the SRS document.
	handlers.OnApproveSRS = func(projectID string) error {
		_, err := coord.ApproveSRS("user")
		return err
	}

	// OnRejectSRS: reject the SRS document with feedback.
	handlers.OnRejectSRS = func(projectID, feedback string) error {
		return coord.RejectSRS(feedback)
	}

	// OnApproveECO: approve an Engineering Change Order.
	handlers.OnApproveECO = func(projectID string, ecoID int64) error {
		return coord.ApproveECO(ecoID, "user")
	}

	// OnRejectECO: reject an Engineering Change Order.
	handlers.OnRejectECO = func(projectID string, ecoID int64) error {
		return coord.RejectECO(ecoID, "user")
	}

	// OnPause: pause the execution loop.
	handlers.OnPause = func(projectID string) error {
		coord.Pause()
		return nil
	}

	// OnResume: resume a paused execution loop.
	handlers.OnResume = func(projectID string) error {
		coord.Resume()
		return nil
	}

	// OnCancel: cancel the project and shut down all containers.
	handlers.OnCancel = func(projectID string) error {
		return coord.Cancel(context.Background())
	}

	// OnGetStatus: query task tree and budget from coordinator.
	handlers.OnGetStatus = func(projectID string) (interface{}, error) {
		db := coord.DB()
		tasks, err := db.ListTasks(state.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}

		report, err := coord.BudgetTracker().GetReport(coord.CompletionPercentage())
		if err != nil {
			return nil, fmt.Errorf("budget report: %w", err)
		}

		return map[string]interface{}{
			"project_id":        projectID,
			"paused":            coord.IsPaused(),
			"active_containers": coord.ActiveContainerCount(),
			"task_count":        len(tasks),
			"budget":            report,
		}, nil
	}

	// OnGetTasks: list all tasks in the project.
	handlers.OnGetTasks = func(projectID string) (interface{}, error) {
		tasks, err := coord.DB().ListTasks(state.TaskFilter{})
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}
		return map[string]interface{}{
			"project_id": projectID,
			"tasks":      tasks,
		}, nil
	}

	// OnGetAttempts: get attempt history for a specific task.
	handlers.OnGetAttempts = func(projectID, taskID string) (interface{}, error) {
		attempts, err := coord.DB().GetTaskAttempts(taskID)
		if err != nil {
			return nil, fmt.Errorf("get attempts: %w", err)
		}
		return map[string]interface{}{
			"task_id":  taskID,
			"attempts": attempts,
		}, nil
	}

	// OnGetCosts: get cost breakdown from the budget tracker.
	handlers.OnGetCosts = func(projectID string) (interface{}, error) {
		report, err := coord.BudgetTracker().GetReport(coord.CompletionPercentage())
		if err != nil {
			return nil, fmt.Errorf("cost report: %w", err)
		}
		return report, nil
	}

	// OnGetEvents: list all events for the project.
	handlers.OnGetEvents = func(projectID string) (interface{}, error) {
		evts, err := coord.DB().ListEvents(state.EventFilter{})
		if err != nil {
			return nil, fmt.Errorf("list events: %w", err)
		}
		return map[string]interface{}{
			"project_id": projectID,
			"events":     evts,
		}, nil
	}

	// OnGetModels: return registered models.
	// Stub for now -- will be wired to model registry when implemented.
	handlers.OnGetModels = func() (interface{}, error) {
		return map[string]interface{}{
			"models": []interface{}{},
		}, nil
	}

	// OnQueryIndex: query the semantic index.
	// Stub for now -- will be wired to semantic indexer when implemented.
	handlers.OnQueryIndex = func(queryType string, params map[string]string) (interface{}, error) {
		return map[string]interface{}{
			"query_type": queryType,
			"results":    []interface{}{},
		}, nil
	}
}
