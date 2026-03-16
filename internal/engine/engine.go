package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// Engine is the core orchestration engine that manages the lifecycle
// of tasks, agents, and the overall execution pipeline.
type Engine struct {
	config  *Config
	db      *state.DB
	emitter *events.Emitter

	mu      sync.Mutex
	running bool
}

// New creates a new Engine instance with the given configuration.
// It initializes the database (runs migrations) and creates the event emitter,
// wiring event persistence so all emitted events are stored in SQLite.
func New(config *Config) (*Engine, error) {
	dbPath := ".axiom/axiom.db"

	db, err := state.NewDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}

	if err := db.RunMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	emitter := events.NewEmitter()

	e := &Engine{
		config:  config,
		db:      db,
		emitter: emitter,
	}

	// Wire event persistence: subscribe to all events and persist to SQLite.
	emitter.SubscribeAll(func(event events.Event) {
		details := ""
		if event.Details != nil {
			details = fmt.Sprintf("%v", event.Details)
		}
		_ = db.InsertEvent(&state.Event{
			Type:      string(event.Type),
			TaskID:    event.TaskID,
			AgentType: event.AgentType,
			AgentID:   event.AgentID,
			Details:   details,
			Timestamp: event.Timestamp,
		})
	})

	return e, nil
}

// Start marks the engine as running and performs crash recovery.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	if err := e.CrashRecovery(); err != nil {
		return fmt.Errorf("crash recovery: %w", err)
	}

	e.running = true
	return nil
}

// Stop marks the engine as stopped and closes the database.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("engine not running")
	}

	e.running = false
	return e.db.Close()
}

// DB returns the state database.
func (e *Engine) DB() *state.DB {
	return e.db
}

// Emitter returns the event emitter.
func (e *Engine) Emitter() *events.Emitter {
	return e.emitter
}

// CrashRecovery resets orphaned tasks and releases stale locks.
// Tasks in 'in_progress' or 'in_review' are reset to 'queued'.
// All locks are released since no containers survive a crash.
func (e *Engine) CrashRecovery() error {
	conn := e.db.Conn()

	// Reset orphaned in_progress tasks to queued
	result, err := conn.Exec("UPDATE tasks SET status = 'queued' WHERE status IN ('in_progress', 'in_review')")
	if err != nil {
		return fmt.Errorf("reset orphaned tasks: %w", err)
	}
	resetCount, _ := result.RowsAffected()

	// Release all locks (no containers survive a crash)
	lockResult, err := conn.Exec("DELETE FROM task_locks")
	if err != nil {
		return fmt.Errorf("release stale locks: %w", err)
	}
	lockCount, _ := lockResult.RowsAffected()

	if resetCount > 0 || lockCount > 0 {
		e.emitter.Emit(events.Event{
			Type: events.EventTaskCreated,
			Details: map[string]interface{}{
				"action":      "crash_recovery",
				"tasks_reset": resetCount,
				"locks_freed": lockCount,
			},
			Timestamp: time.Now(),
		})
	}

	return nil
}
