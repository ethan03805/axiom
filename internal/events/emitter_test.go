package events

import (
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndEmit(t *testing.T) {
	emitter := NewEmitter()

	var mu sync.Mutex
	var received []Event

	emitter.Subscribe(EventTaskCreated, func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, event)
	})

	// Emit a task_created event
	emitter.Emit(Event{
		Type:   EventTaskCreated,
		TaskID: "test-task-1",
		Details: map[string]interface{}{
			"title": "Test Task",
		},
	})

	// Emit a different event type -- should not be received
	emitter.Emit(Event{
		Type:   EventTaskFailed,
		TaskID: "test-task-2",
	})

	// Wait for goroutines to complete
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].TaskID != "test-task-1" {
		t.Errorf("expected task ID test-task-1, got %s", received[0].TaskID)
	}
	if received[0].Type != EventTaskCreated {
		t.Errorf("expected type task_created, got %s", received[0].Type)
	}
}

func TestSubscribeAll(t *testing.T) {
	emitter := NewEmitter()

	var mu sync.Mutex
	var received []Event

	emitter.SubscribeAll(func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, event)
	})

	emitter.Emit(Event{Type: EventTaskCreated, TaskID: "t1"})
	emitter.Emit(Event{Type: EventTaskFailed, TaskID: "t2"})
	emitter.Emit(Event{Type: EventBudgetWarning, TaskID: "t3"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
}

func TestEmitSetsTimestamp(t *testing.T) {
	emitter := NewEmitter()

	var mu sync.Mutex
	var received Event

	emitter.Subscribe(EventTaskStarted, func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		received = event
	})

	before := time.Now()
	emitter.Emit(Event{
		Type:   EventTaskStarted,
		TaskID: "ts-test",
	})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if received.Timestamp.Before(before) {
		t.Error("expected timestamp to be set to current time")
	}
}
