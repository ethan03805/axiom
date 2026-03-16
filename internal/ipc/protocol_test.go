package ipc

import (
	"encoding/json"
	"testing"
)

func TestParseMessageType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected MessageType
		wantErr  bool
	}{
		{
			name:     "inference request",
			input:    `{"type": "inference_request", "task_id": "task-042"}`,
			expected: TypeInferenceRequest,
		},
		{
			name:     "task output",
			input:    `{"type": "task_output", "task_id": "task-001"}`,
			expected: TypeTaskOutput,
		},
		{
			name:     "scope expansion",
			input:    `{"type": "request_scope_expansion", "task_id": "task-042"}`,
			expected: TypeScopeExpansionRequest,
		},
		{
			name:    "missing type",
			input:   `{"task_id": "task-001"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgType, err := ParseMessageType([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msgType != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, msgType)
			}
		})
	}
}

func TestParseInferenceRequest(t *testing.T) {
	// Test the exact format from Architecture Section 19.5.
	raw := `{
		"type": "inference_request",
		"task_id": "task-042",
		"model_id": "anthropic/claude-4-sonnet",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Write a function."}
		],
		"max_tokens": 8192,
		"temperature": 0.2,
		"grammar_constraints": null
	}`

	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	req, ok := msg.(*InferenceRequestMessage)
	if !ok {
		t.Fatalf("expected *InferenceRequestMessage, got %T", msg)
	}

	if req.Type != TypeInferenceRequest {
		t.Errorf("type = %s, want %s", req.Type, TypeInferenceRequest)
	}
	if req.TaskID != "task-042" {
		t.Errorf("task_id = %s, want task-042", req.TaskID)
	}
	if req.ModelID != "anthropic/claude-4-sonnet" {
		t.Errorf("model_id = %s", req.ModelID)
	}
	if len(req.Messages) != 2 {
		t.Errorf("messages count = %d, want 2", len(req.Messages))
	}
	if req.MaxTokens != 8192 {
		t.Errorf("max_tokens = %d, want 8192", req.MaxTokens)
	}
	if req.Temperature != 0.2 {
		t.Errorf("temperature = %f, want 0.2", req.Temperature)
	}
	if req.GrammarConstraints != nil {
		t.Errorf("grammar_constraints should be nil")
	}
}

func TestParseScopeExpansionRequest(t *testing.T) {
	// Test the exact format from Architecture Section 10.7.
	raw := `{
		"type": "request_scope_expansion",
		"task_id": "task-042",
		"additional_files": [
			"src/routes/api.go",
			"src/middleware/cors.go"
		],
		"reason": "Need to update API route registration to match new handler signature"
	}`

	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	req, ok := msg.(*ScopeExpansionRequestMessage)
	if !ok {
		t.Fatalf("expected *ScopeExpansionRequestMessage, got %T", msg)
	}

	if req.TaskID != "task-042" {
		t.Errorf("task_id = %s", req.TaskID)
	}
	if len(req.AdditionalFiles) != 2 {
		t.Fatalf("additional_files count = %d, want 2", len(req.AdditionalFiles))
	}
	if req.AdditionalFiles[0] != "src/routes/api.go" {
		t.Errorf("file[0] = %s", req.AdditionalFiles[0])
	}
	if req.Reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestParseScopeExpansionResponse(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		status string
	}{
		{
			name: "approved",
			raw: `{
				"type": "scope_expansion_response",
				"task_id": "task-042",
				"status": "approved",
				"expanded_files": ["src/routes/api.go", "src/middleware/cors.go"],
				"locks_acquired": true
			}`,
			status: "approved",
		},
		{
			name: "waiting on lock",
			raw: `{
				"type": "scope_expansion_response",
				"task_id": "task-042",
				"status": "waiting_on_lock",
				"blocked_by": "task-038",
				"message": "Container will be destroyed and task re-queued when locks are available"
			}`,
			status: "waiting_on_lock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessage([]byte(tt.raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			resp, ok := msg.(*ScopeExpansionResponseMessage)
			if !ok {
				t.Fatalf("expected *ScopeExpansionResponseMessage, got %T", msg)
			}
			if resp.Status != tt.status {
				t.Errorf("status = %s, want %s", resp.Status, tt.status)
			}
		})
	}
}

func TestAllMessageTypesRoundTrip(t *testing.T) {
	// Verify every message type serializes and deserializes correctly.
	messages := []interface{}{
		&TaskSpecMessage{
			Header: Header{Type: TypeTaskSpec, TaskID: "t1"},
			Spec:   "# TaskSpec content",
		},
		&ReviewSpecMessage{
			Header: Header{Type: TypeReviewSpec, TaskID: "t2"},
			Spec:   "# ReviewSpec content",
		},
		&RevisionRequestMessage{
			Header:       Header{Type: TypeRevisionRequest, TaskID: "t3"},
			OriginalSpec: "spec",
			Feedback:     "fix the bug",
			FailureType:  "validation",
			AttemptNumber: 2,
		},
		&TaskOutputMessage{
			Header:       Header{Type: TypeTaskOutput, TaskID: "t4"},
			BaseSnapshot: "abc123",
			Manifest:     json.RawMessage(`{"files":{}}`),
		},
		&ReviewResultMessage{
			Header:     Header{Type: TypeReviewResult, TaskID: "t5"},
			Verdict:    "approve",
			Feedback:   "",
			Evaluation: "all criteria pass",
		},
		&InferenceRequestMessage{
			Header:    Header{Type: TypeInferenceRequest, TaskID: "t6"},
			ModelID:   "anthropic/claude-4-sonnet",
			Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
			MaxTokens: 1024,
		},
		&InferenceResponseMessage{
			Header:       Header{Type: TypeInferenceResponse, TaskID: "t7"},
			Content:      "response text",
			InputTokens:  100,
			OutputTokens: 50,
			FinishReason: "stop",
		},
		&LateralMessage{
			Header:      Header{Type: TypeLateralMessage, TaskID: "t8"},
			FromAgentID: "agent-1",
			ToAgentID:   "agent-2",
			Scope:       "api-contract",
			Payload:     json.RawMessage(`{"endpoint":"/api/users"}`),
		},
		&ActionRequestMessage{
			Header:     Header{Type: TypeActionRequest, TaskID: "t9"},
			Action:     "spawn_meeseeks",
			Parameters: json.RawMessage(`{"tier":"standard"}`),
		},
		&ActionResponseMessage{
			Header:  Header{Type: TypeActionResponse, TaskID: "t10"},
			Action:  "spawn_meeseeks",
			Success: true,
			Result:  json.RawMessage(`{"container_id":"abc"}`),
		},
		&ScopeExpansionRequestMessage{
			Header:          Header{Type: TypeScopeExpansionRequest, TaskID: "t11"},
			AdditionalFiles: []string{"src/main.go"},
			Reason:          "need to modify entry point",
		},
		&ScopeExpansionResponseMessage{
			Header:        Header{Type: TypeScopeExpansionResponse, TaskID: "t12"},
			Status:        "approved",
			ExpandedFiles: []string{"src/main.go"},
			LocksAcquired: true,
		},
		&ContextInvalidationMessage{
			Header:         Header{Type: TypeContextInvalidation, TaskID: "t13"},
			ChangedSymbols: []string{"HandleAuth", "UserService"},
			CommitSHA:      "def456",
			Message:        "symbols changed by task-007",
		},
		&ShutdownMessage{
			Header: Header{Type: TypeShutdown, TaskID: "t14"},
			Reason: "task completed",
		},
	}

	for _, original := range messages {
		data, err := MarshalMessage(original)
		if err != nil {
			t.Fatalf("marshal %T: %v", original, err)
		}

		parsed, err := ParseMessage(data)
		if err != nil {
			t.Fatalf("parse %T: %v", original, err)
		}

		// Re-marshal and compare JSON to verify round-trip.
		data2, err := MarshalMessage(parsed)
		if err != nil {
			t.Fatalf("re-marshal %T: %v", parsed, err)
		}

		if string(data) != string(data2) {
			t.Errorf("round-trip mismatch for %T:\n  original: %s\n  parsed:   %s", original, data, data2)
		}
	}
}

func TestParseUnknownMessageType(t *testing.T) {
	raw := `{"type": "unknown_type", "task_id": "t1"}`
	_, err := ParseMessage([]byte(raw))
	if err == nil {
		t.Error("expected error for unknown message type")
	}
}
