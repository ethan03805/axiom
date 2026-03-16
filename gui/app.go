// Package gui implements the Wails v2 desktop application for the Axiom
// GUI Dashboard. The Go backend provides bindings for engine operations
// that the React frontend calls.
//
// See Architecture.md Section 26 for the full specification.
package gui

import (
	"context"

	"github.com/ethan03805/axiom/internal/events"
)

// App is the Wails application backend. It exposes engine operations
// to the React frontend via Go-to-JS bindings.
type App struct {
	ctx     context.Context
	emitter *events.Emitter

	// Callbacks wired by the engine.
	OnNewProject    func(prompt string, budget float64) (string, error)
	OnApproveSRS    func() error
	OnRejectSRS     func(feedback string) error
	OnApproveECO    func(ecoID int64) error
	OnRejectECO     func(ecoID int64) error
	OnPause         func() error
	OnResume        func() error
	OnCancel        func() error
	OnSetBudget     func(amount float64) error
	OnBitNetStart   func() error
	OnBitNetStop    func() error
	OnTunnelStart   func() (string, error)
	OnTunnelStop    func() error
	OnGetStatus     func() (interface{}, error)
	OnGetTasks      func() (interface{}, error)
	OnGetCosts      func() (interface{}, error)
	OnGetContainers func() (interface{}, error)
	OnGetEvents     func(limit int) (interface{}, error)
	OnGetModels     func() (interface{}, error)
}

// NewApp creates a new Wails application backend.
func NewApp(emitter *events.Emitter) *App {
	return &App{
		emitter: emitter,
	}
}

// Startup is called when the Wails app starts.
// The context is used for Wails runtime calls.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// Subscribe to all engine events and forward to the frontend.
	// The React frontend listens for "axiom:event" events via Wails runtime.
	// See Architecture Section 26.4.
	a.emitter.SubscribeAll(func(event events.Event) {
		// In a real Wails app, this would call:
		// runtime.EventsEmit(a.ctx, "axiom:event", event)
		_ = event
	})
}

// --- Bindings exposed to React frontend ---

// NewProject creates a new project with the given prompt and budget.
func (a *App) NewProject(prompt string, budget float64) (string, error) {
	if a.OnNewProject != nil {
		return a.OnNewProject(prompt, budget)
	}
	return "", nil
}

// ApproveSRS approves the current SRS.
func (a *App) ApproveSRS() error {
	if a.OnApproveSRS != nil {
		return a.OnApproveSRS()
	}
	return nil
}

// RejectSRS rejects the current SRS with feedback.
func (a *App) RejectSRS(feedback string) error {
	if a.OnRejectSRS != nil {
		return a.OnRejectSRS(feedback)
	}
	return nil
}

// ApproveECO approves an Engineering Change Order.
func (a *App) ApproveECO(ecoID int64) error {
	if a.OnApproveECO != nil {
		return a.OnApproveECO(ecoID)
	}
	return nil
}

// RejectECO rejects an Engineering Change Order.
func (a *App) RejectECO(ecoID int64) error {
	if a.OnRejectECO != nil {
		return a.OnRejectECO(ecoID)
	}
	return nil
}

// Pause pauses project execution.
func (a *App) Pause() error {
	if a.OnPause != nil {
		return a.OnPause()
	}
	return nil
}

// Resume resumes project execution.
func (a *App) Resume() error {
	if a.OnResume != nil {
		return a.OnResume()
	}
	return nil
}

// Cancel cancels project execution.
func (a *App) Cancel() error {
	if a.OnCancel != nil {
		return a.OnCancel()
	}
	return nil
}

// SetBudget updates the project budget.
func (a *App) SetBudget(amount float64) error {
	if a.OnSetBudget != nil {
		return a.OnSetBudget(amount)
	}
	return nil
}

// BitNetStart starts the local inference server.
func (a *App) BitNetStart() error {
	if a.OnBitNetStart != nil {
		return a.OnBitNetStart()
	}
	return nil
}

// BitNetStop stops the local inference server.
func (a *App) BitNetStop() error {
	if a.OnBitNetStop != nil {
		return a.OnBitNetStop()
	}
	return nil
}

// TunnelStart starts the Cloudflare Tunnel.
func (a *App) TunnelStart() (string, error) {
	if a.OnTunnelStart != nil {
		return a.OnTunnelStart()
	}
	return "", nil
}

// TunnelStop stops the Cloudflare Tunnel.
func (a *App) TunnelStop() error {
	if a.OnTunnelStop != nil {
		return a.OnTunnelStop()
	}
	return nil
}

// GetStatus returns the current project status.
func (a *App) GetStatus() (interface{}, error) {
	if a.OnGetStatus != nil {
		return a.OnGetStatus()
	}
	return map[string]string{"status": "idle"}, nil
}

// GetTasks returns the task tree.
func (a *App) GetTasks() (interface{}, error) {
	if a.OnGetTasks != nil {
		return a.OnGetTasks()
	}
	return []interface{}{}, nil
}

// GetCosts returns the cost breakdown.
func (a *App) GetCosts() (interface{}, error) {
	if a.OnGetCosts != nil {
		return a.OnGetCosts()
	}
	return map[string]float64{"total": 0}, nil
}

// GetContainers returns active containers.
func (a *App) GetContainers() (interface{}, error) {
	if a.OnGetContainers != nil {
		return a.OnGetContainers()
	}
	return []interface{}{}, nil
}

// GetEvents returns recent events.
func (a *App) GetEvents(limit int) (interface{}, error) {
	if a.OnGetEvents != nil {
		return a.OnGetEvents(limit)
	}
	return []interface{}{}, nil
}

// GetModels returns the model registry.
func (a *App) GetModels() (interface{}, error) {
	if a.OnGetModels != nil {
		return a.OnGetModels()
	}
	return []interface{}{}, nil
}
