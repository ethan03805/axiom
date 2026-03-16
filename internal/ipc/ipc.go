package ipc

import (
	"context"
)

// Transport defines the interface for inter-process communication
// between the orchestrator and agent containers.
type Transport interface {
	Send(ctx context.Context, msg *Message) error
	Receive(ctx context.Context) (*Message, error)
	Close() error
}

// Message represents a message exchanged between orchestrator and agent.
type Message struct {
	ID      string
	Type    MessageType
	TaskID  string
	AgentID string
	Payload []byte
}

// MessageType identifies the kind of IPC message.
type MessageType string

const (
	MessageTypeTaskAssign   MessageType = "task_assign"
	MessageTypeTaskResult   MessageType = "task_result"
	MessageTypeHeartbeat    MessageType = "heartbeat"
	MessageTypeShutdown     MessageType = "shutdown"
	MessageTypeFileSync     MessageType = "file_sync"
	MessageTypeValidation   MessageType = "validation"
)

// StdioTransport implements Transport over stdin/stdout pipes.
type StdioTransport struct{}

// NewStdioTransport creates a new stdio-based IPC transport.
func NewStdioTransport() *StdioTransport {
	return &StdioTransport{}
}

// Send sends a message over the transport.
func (t *StdioTransport) Send(ctx context.Context, msg *Message) error {
	return nil
}

// Receive waits for and returns the next message.
func (t *StdioTransport) Receive(ctx context.Context) (*Message, error) {
	return nil, nil
}

// Close closes the transport.
func (t *StdioTransport) Close() error {
	return nil
}
