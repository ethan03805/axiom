package gui

import (
	"testing"

	"github.com/ethan03805/axiom/internal/events"
)

func TestAppBindingsDefaultReturnValues(t *testing.T) {
	emitter := events.NewEmitter()
	app := NewApp(emitter)

	// All bindings should return defaults when no callbacks are set.
	if _, err := app.GetStatus(); err != nil {
		t.Errorf("GetStatus: %v", err)
	}
	if _, err := app.GetTasks(); err != nil {
		t.Errorf("GetTasks: %v", err)
	}
	if _, err := app.GetCosts(); err != nil {
		t.Errorf("GetCosts: %v", err)
	}
	if _, err := app.GetContainers(); err != nil {
		t.Errorf("GetContainers: %v", err)
	}
	if _, err := app.GetEvents(10); err != nil {
		t.Errorf("GetEvents: %v", err)
	}
	if _, err := app.GetModels(); err != nil {
		t.Errorf("GetModels: %v", err)
	}
}

func TestAppBindingsWithCallbacks(t *testing.T) {
	emitter := events.NewEmitter()
	app := NewApp(emitter)

	var paused, resumed, cancelled bool
	app.OnPause = func() error { paused = true; return nil }
	app.OnResume = func() error { resumed = true; return nil }
	app.OnCancel = func() error { cancelled = true; return nil }

	app.Pause()
	app.Resume()
	app.Cancel()

	if !paused {
		t.Error("Pause callback not called")
	}
	if !resumed {
		t.Error("Resume callback not called")
	}
	if !cancelled {
		t.Error("Cancel callback not called")
	}
}

func TestAppNewProject(t *testing.T) {
	app := NewApp(events.NewEmitter())

	var receivedPrompt string
	var receivedBudget float64
	app.OnNewProject = func(prompt string, budget float64) (string, error) {
		receivedPrompt = prompt
		receivedBudget = budget
		return "proj-123", nil
	}

	id, err := app.NewProject("Build a REST API", 10.0)
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	if id != "proj-123" {
		t.Errorf("id = %s", id)
	}
	if receivedPrompt != "Build a REST API" {
		t.Errorf("prompt = %s", receivedPrompt)
	}
	if receivedBudget != 10.0 {
		t.Errorf("budget = %f", receivedBudget)
	}
}

func TestAppSRSApproval(t *testing.T) {
	app := NewApp(events.NewEmitter())

	var approved bool
	app.OnApproveSRS = func() error { approved = true; return nil }

	app.ApproveSRS()
	if !approved {
		t.Error("ApproveSRS callback not called")
	}
}

func TestAppSRSRejection(t *testing.T) {
	app := NewApp(events.NewEmitter())

	var feedback string
	app.OnRejectSRS = func(fb string) error { feedback = fb; return nil }

	app.RejectSRS("Missing requirements section")
	if feedback != "Missing requirements section" {
		t.Errorf("feedback = %s", feedback)
	}
}

func TestAppBudgetModification(t *testing.T) {
	app := NewApp(events.NewEmitter())

	var newBudget float64
	app.OnSetBudget = func(amount float64) error { newBudget = amount; return nil }

	app.SetBudget(25.0)
	if newBudget != 25.0 {
		t.Errorf("budget = %f", newBudget)
	}
}
