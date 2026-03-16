package events

import (
	"sync"
	"time"
)

// Event represents a system event.
type Event struct {
	ID        int64
	Type      string
	TaskID    string
	AgentType string
	AgentID   string
	Details   string
	Timestamp time.Time
}

// Handler is a function that processes events.
type Handler func(event *Event)

// Bus provides a publish-subscribe event system.
type Bus struct {
	handlers map[string][]Handler
	mu       sync.RWMutex
}

// New creates a new event Bus.
func New() *Bus {
	return &Bus{
		handlers: make(map[string][]Handler),
	}
}

// Subscribe registers a handler for the given event type.
func (b *Bus) Subscribe(eventType string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish sends an event to all registered handlers for its type.
func (b *Bus) Publish(event *Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

// PublishAsync sends an event to handlers asynchronously.
func (b *Bus) PublishAsync(event *Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go h(event)
	}
}
