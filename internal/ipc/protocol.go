// Package ipc implements filesystem-based inter-process communication between
// the Trusted Engine and Docker containers in the Untrusted Agent Plane.
//
// Communication uses JSON files written to mounted IPC directories:
//   - Engine -> Container: /workspace/ipc/input/  (host: .axiom/containers/ipc/<task-id>/input/)
//   - Container -> Engine: /workspace/ipc/output/ (host: .axiom/containers/ipc/<task-id>/output/)
//
// See Architecture.md Section 20 for the full communication model.
package ipc

import (
	"encoding/json"
	"fmt"
)

// MessageType identifies the kind of IPC message.
// All 14 message types from Architecture Section 20.4.
type MessageType string

const (
	// Engine -> Meeseeks: deliver TaskSpec for execution.
	TypeTaskSpec MessageType = "task_spec"

	// Engine -> Reviewer: deliver ReviewSpec for evaluation.
	TypeReviewSpec MessageType = "review_spec"

	// Engine -> Meeseeks: return feedback for revision.
	TypeRevisionRequest MessageType = "revision_request"

	// Meeseeks -> Engine: submit completed work + manifest.
	TypeTaskOutput MessageType = "task_output"

	// Reviewer -> Engine: submit review verdict.
	TypeReviewResult MessageType = "review_result"

	// Any Agent -> Engine: request model inference.
	TypeInferenceRequest MessageType = "inference_request"

	// Engine -> Any Agent: return inference result.
	TypeInferenceResponse MessageType = "inference_response"

	// Engine <-> Meeseeks: brokered lateral communication.
	TypeLateralMessage MessageType = "lateral_message"

	// Agent -> Engine: request privileged action.
	TypeActionRequest MessageType = "action_request"

	// Engine -> Agent: return action result.
	TypeActionResponse MessageType = "action_response"

	// Meeseeks -> Engine: request additional files outside declared scope.
	TypeScopeExpansionRequest MessageType = "request_scope_expansion"

	// Engine -> Meeseeks: approval or denial of scope expansion.
	TypeScopeExpansionResponse MessageType = "scope_expansion_response"

	// Engine -> Meeseeks: warning that referenced symbols have changed.
	TypeContextInvalidation MessageType = "context_invalidation_warning"

	// Engine -> Container: request graceful container shutdown.
	TypeShutdown MessageType = "shutdown"
)

// Header contains the common fields present in every IPC message.
// All concrete message types embed this header.
type Header struct {
	Type   MessageType `json:"type"`
	TaskID string      `json:"task_id,omitempty"`
}

// --- Engine -> Meeseeks messages ---

// TaskSpecMessage delivers a TaskSpec to a Meeseeks for execution.
// See Architecture Section 10.3.
type TaskSpecMessage struct {
	Header
	Spec string `json:"spec"` // Full TaskSpec content (markdown)
}

// ReviewSpecMessage delivers a ReviewSpec to a reviewer for evaluation.
// See Architecture Section 11.7.
type ReviewSpecMessage struct {
	Header
	Spec string `json:"spec"` // Full ReviewSpec content (markdown)
}

// RevisionRequestMessage sends feedback to a Meeseeks for revision.
// Contains the original spec plus structured feedback from validation,
// reviewer, or orchestrator rejection.
type RevisionRequestMessage struct {
	Header
	OriginalSpec string `json:"original_spec"`
	Feedback     string `json:"feedback"`
	FailureType  string `json:"failure_type"` // "validation" | "reviewer" | "orchestrator"
	AttemptNumber int   `json:"attempt_number"`
}

// --- Container -> Engine messages ---

// TaskOutputMessage submits completed work from a Meeseeks.
// The actual files are in /workspace/staging/; this message signals completion
// and includes the manifest. See Architecture Section 10.4.
type TaskOutputMessage struct {
	Header
	BaseSnapshot string          `json:"base_snapshot"`
	Manifest     json.RawMessage `json:"manifest"` // manifest.json content
}

// ReviewResultMessage submits a review verdict from a reviewer.
// See Architecture Section 11.7.
type ReviewResultMessage struct {
	Header
	Verdict    string `json:"verdict"`  // "approve" | "reject"
	Feedback   string `json:"feedback"` // Specific feedback if rejected
	Evaluation string `json:"evaluation"` // Per-criterion evaluation
}

// --- Inference messages ---

// ChatMessage represents a single message in a chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`    // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// InferenceRequestMessage requests model inference from the engine's broker.
// See Architecture Section 19.5.
type InferenceRequestMessage struct {
	Header
	ModelID            string        `json:"model_id"`
	Messages           []ChatMessage `json:"messages"`
	MaxTokens          int           `json:"max_tokens"`
	Temperature        float64       `json:"temperature"`
	GrammarConstraints *string       `json:"grammar_constraints"` // GBNF grammar, nil if unconstrained
}

// InferenceResponseMessage returns the inference result to the requesting agent.
type InferenceResponseMessage struct {
	Header
	ModelID      string `json:"model_id"`
	Content      string `json:"content"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	FinishReason string `json:"finish_reason"` // "stop" | "length" | "error"
	Error        string `json:"error,omitempty"`
}

// InferenceStreamChunk is a partial inference response used for streaming.
// Chunks are written as sequential files: response-001.json, response-002.json, etc.
// See Architecture Section 19.5 (Streaming).
type InferenceStreamChunk struct {
	Header
	ChunkIndex   int    `json:"chunk_index"`
	Content      string `json:"content"`       // Partial content delta
	Done         bool   `json:"done"`          // True for the final chunk
	InputTokens  int    `json:"input_tokens"`  // Set only on final chunk
	OutputTokens int    `json:"output_tokens"` // Set only on final chunk
	FinishReason string `json:"finish_reason"` // Set only on final chunk
}

// --- Lateral communication ---

// LateralMessage is an engine-brokered message between Meeseeks agents.
// See Architecture Section 20.2.
type LateralMessage struct {
	Header
	FromAgentID string          `json:"from_agent_id"`
	ToAgentID   string          `json:"to_agent_id"`
	Scope       string          `json:"scope"` // Communication scope description
	Payload     json.RawMessage `json:"payload"`
}

// --- Action request/response ---

// ActionRequestMessage requests a privileged action from the engine.
// Used by orchestrators and sub-orchestrators to request operations like
// spawning Meeseeks, querying the index, etc.
// See Architecture Section 8.6 for the full list of request types.
type ActionRequestMessage struct {
	Header
	Action     string          `json:"action"` // e.g. "spawn_meeseeks", "create_task", "query_index"
	Parameters json.RawMessage `json:"parameters"`
}

// ActionResponseMessage returns the result of a privileged action request.
type ActionResponseMessage struct {
	Header
	Action  string          `json:"action"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// --- Scope expansion ---

// ScopeExpansionRequestMessage requests modification of files outside the
// Meeseeks' originally declared target scope.
// See Architecture Section 10.7.
type ScopeExpansionRequestMessage struct {
	Header
	AdditionalFiles []string `json:"additional_files"`
	Reason          string   `json:"reason"`
}

// ScopeExpansionResponseMessage responds to a scope expansion request.
// Status can be "approved", "denied", or "waiting_on_lock".
// See Architecture Section 10.7 for the three response scenarios.
type ScopeExpansionResponseMessage struct {
	Header
	Status        string   `json:"status"` // "approved" | "denied" | "waiting_on_lock"
	ExpandedFiles []string `json:"expanded_files,omitempty"`
	LocksAcquired bool     `json:"locks_acquired,omitempty"`
	BlockedBy     string   `json:"blocked_by,omitempty"` // Task ID holding conflicting locks
	Message       string   `json:"message,omitempty"`
}

// --- Context invalidation ---

// ContextInvalidationMessage warns a Meeseeks that symbols referenced in
// its TaskSpec context have been changed by a recently committed task.
// See Architecture Section 16.5.
type ContextInvalidationMessage struct {
	Header
	ChangedSymbols []string `json:"changed_symbols"`
	CommitSHA      string   `json:"commit_sha"`
	Message        string   `json:"message"`
}

// --- Shutdown ---

// ShutdownMessage requests graceful container shutdown.
type ShutdownMessage struct {
	Header
	Reason string `json:"reason"`
}

// ParseMessageType reads only the "type" field from a raw JSON message
// to determine which concrete struct to unmarshal into.
func ParseMessageType(data []byte) (MessageType, error) {
	var h Header
	if err := json.Unmarshal(data, &h); err != nil {
		return "", fmt.Errorf("parse message type: %w", err)
	}
	if h.Type == "" {
		return "", fmt.Errorf("message missing 'type' field")
	}
	return h.Type, nil
}

// ParseMessage unmarshals raw JSON into the appropriate concrete message struct
// based on the "type" field. Returns the parsed message as an interface{}.
func ParseMessage(data []byte) (interface{}, error) {
	msgType, err := ParseMessageType(data)
	if err != nil {
		return nil, err
	}

	var msg interface{}
	switch msgType {
	case TypeTaskSpec:
		msg = &TaskSpecMessage{}
	case TypeReviewSpec:
		msg = &ReviewSpecMessage{}
	case TypeRevisionRequest:
		msg = &RevisionRequestMessage{}
	case TypeTaskOutput:
		msg = &TaskOutputMessage{}
	case TypeReviewResult:
		msg = &ReviewResultMessage{}
	case TypeInferenceRequest:
		msg = &InferenceRequestMessage{}
	case TypeInferenceResponse:
		msg = &InferenceResponseMessage{}
	case TypeLateralMessage:
		msg = &LateralMessage{}
	case TypeActionRequest:
		msg = &ActionRequestMessage{}
	case TypeActionResponse:
		msg = &ActionResponseMessage{}
	case TypeScopeExpansionRequest:
		msg = &ScopeExpansionRequestMessage{}
	case TypeScopeExpansionResponse:
		msg = &ScopeExpansionResponseMessage{}
	case TypeContextInvalidation:
		msg = &ContextInvalidationMessage{}
	case TypeShutdown:
		msg = &ShutdownMessage{}
	default:
		return nil, fmt.Errorf("unknown message type: %s", msgType)
	}

	if err := json.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("unmarshal %s message: %w", msgType, err)
	}
	return msg, nil
}

// MarshalMessage serializes any IPC message struct to JSON bytes.
func MarshalMessage(msg interface{}) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	return data, nil
}
