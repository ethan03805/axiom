package events

import (
	"sync"
	"time"
)

// EventType identifies the kind of event being emitted.
type EventType string

const (
	EventTaskCreated             EventType = "task_created"
	EventTaskStarted             EventType = "task_started"
	EventTaskCompleted           EventType = "task_completed"
	EventTaskFailed              EventType = "task_failed"
	EventTaskBlocked             EventType = "task_blocked"
	EventContainerSpawned        EventType = "container_spawned"
	EventContainerDestroyed      EventType = "container_destroyed"
	EventReviewStarted           EventType = "review_started"
	EventReviewCompleted         EventType = "review_completed"
	EventMergeStarted            EventType = "merge_started"
	EventMergeCompleted          EventType = "merge_completed"
	EventBudgetWarning           EventType = "budget_warning"
	EventBudgetExhausted         EventType = "budget_exhausted"
	EventECOProposed             EventType = "eco_proposed"
	EventECOApproved             EventType = "eco_approved"
	EventECORejected             EventType = "eco_rejected"
	EventSRSSubmitted            EventType = "srs_submitted"
	EventSRSApproved             EventType = "srs_approved"
	EventScopeExpansionRequested EventType = "scope_expansion_requested"
	EventScopeExpansionApproved  EventType = "scope_expansion_approved"
	EventScopeExpansionDenied    EventType = "scope_expansion_denied"
	EventContextInvalidation     EventType = "context_invalidation_warning"
	EventProviderUnavailable     EventType = "provider_unavailable"
	EventCrashRecovery           EventType = "crash_recovery"
)

// Event represents a system event that can be emitted and subscribed to.
type Event struct {
	Type      EventType
	TaskID    string
	AgentType string
	AgentID   string
	Details   map[string]interface{}
	Timestamp time.Time
}

// Subscriber is a function that handles an event.
type Subscriber func(event Event)

// Emitter provides a thread-safe publish-subscribe event system.
type Emitter struct {
	mu          sync.RWMutex
	subscribers map[EventType][]Subscriber
	allSubs     []Subscriber
}

// NewEmitter creates a new Emitter with initialized subscriber maps.
func NewEmitter() *Emitter {
	return &Emitter{
		subscribers: make(map[EventType][]Subscriber),
	}
}

// Subscribe registers a handler for a specific event type.
func (e *Emitter) Subscribe(eventType EventType, fn Subscriber) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.subscribers[eventType] = append(e.subscribers[eventType], fn)
}

// SubscribeAll registers a handler that receives all events.
func (e *Emitter) SubscribeAll(fn Subscriber) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.allSubs = append(e.allSubs, fn)
}

// Emit dispatches an event to all matching subscribers asynchronously.
// If the event has no timestamp set, it defaults to time.Now().
func (e *Emitter) Emit(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	e.mu.RLock()
	typed := make([]Subscriber, len(e.subscribers[event.Type]))
	copy(typed, e.subscribers[event.Type])
	all := make([]Subscriber, len(e.allSubs))
	copy(all, e.allSubs)
	e.mu.RUnlock()

	for _, fn := range typed {
		go fn(event)
	}
	for _, fn := range all {
		go fn(event)
	}
}
