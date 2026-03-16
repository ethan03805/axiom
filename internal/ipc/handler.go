package ipc

import (
	"fmt"
	"sync"
)

// HandlerFunc processes a specific IPC message type. It receives the task ID,
// the parsed message, and the raw JSON bytes. It returns a response message
// (which will be written back to the container's input directory), or nil if
// no response is needed.
type HandlerFunc func(taskID string, msg interface{}, raw []byte) (response interface{}, err error)

// Dispatcher routes incoming IPC messages to the appropriate handler based
// on message type. Each engine subsystem registers its handler for the
// message types it processes.
//
// Routing per BUILD_PLAN step 3.4:
//   - inference_request     -> Inference Broker
//   - task_output           -> File Router / Approval Pipeline
//   - review_result         -> Pipeline stage advancement
//   - action_request        -> Action dispatcher (spawn, query, etc.)
//   - request_scope_expansion -> Scope expansion handler
//
// See Architecture Section 20.4 for the complete message type table.
type Dispatcher struct {
	writer   *Writer
	mu       sync.RWMutex
	handlers map[MessageType]HandlerFunc
	fallback HandlerFunc // Called for unregistered message types
}

// NewDispatcher creates a Dispatcher that writes responses using the given Writer.
func NewDispatcher(writer *Writer) *Dispatcher {
	return &Dispatcher{
		writer:   writer,
		handlers: make(map[MessageType]HandlerFunc),
	}
}

// Register associates a handler function with a message type.
// Only one handler per message type is allowed; registering a second
// handler for the same type replaces the first.
func (d *Dispatcher) Register(msgType MessageType, handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[msgType] = handler
}

// SetFallback sets a handler for unregistered message types.
// This is useful for logging unexpected messages without failing.
func (d *Dispatcher) SetFallback(handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fallback = handler
}

// Dispatch processes an incoming IPC message by looking up and calling
// the registered handler for its message type. If the handler returns a
// response, it is written back to the container's input directory.
//
// This method is designed to be used as the MessageHandler callback
// for the Watcher.
func (d *Dispatcher) Dispatch(taskID string, msg interface{}, raw []byte) {
	// Determine the message type.
	msgType, err := ParseMessageType(raw)
	if err != nil {
		return
	}

	// Look up the handler.
	d.mu.RLock()
	handler, ok := d.handlers[msgType]
	if !ok {
		handler = d.fallback
	}
	d.mu.RUnlock()

	if handler == nil {
		return // No handler registered and no fallback
	}

	// Call the handler.
	response, err := handler(taskID, msg, raw)
	if err != nil {
		// Write an error response back to the container.
		errResp := &ActionResponseMessage{
			Header:  Header{Type: TypeActionResponse, TaskID: taskID},
			Success: false,
			Error:   fmt.Sprintf("handler error: %v", err),
		}
		_ = d.writer.Send(taskID, errResp)
		return
	}

	// Write the response back to the container if one was returned.
	if response != nil {
		_ = d.writer.Send(taskID, response)
	}
}

// HandlersRegistered returns the list of message types that have handlers.
func (d *Dispatcher) HandlersRegistered() []MessageType {
	d.mu.RLock()
	defer d.mu.RUnlock()

	types := make([]MessageType, 0, len(d.handlers))
	for t := range d.handlers {
		types = append(types, t)
	}
	return types
}
