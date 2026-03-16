package broker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name       string
	available  bool
	response   *InferenceResponse
	err        error
	callCount  int
	streamResp *InferenceResponse
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Available(_ context.Context) bool { return m.available }

func (m *mockProvider) Complete(_ context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &InferenceResponse{
		Content:      "mock response",
		ModelID:      req.ModelID,
		InputTokens:  100,
		OutputTokens: 50,
		FinishReason: "stop",
	}, nil
}

func (m *mockProvider) CompleteStream(_ context.Context, req *InferenceRequest, onChunk func(chunk string, done bool)) (*InferenceResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	// Simulate 3 chunks.
	onChunk("chunk1 ", false)
	onChunk("chunk2 ", false)
	onChunk("chunk3", true)

	if m.streamResp != nil {
		return m.streamResp, nil
	}
	return &InferenceResponse{
		Content:      "chunk1 chunk2 chunk3",
		ModelID:      req.ModelID,
		InputTokens:  100,
		OutputTokens: 50,
		FinishReason: "stop",
	}, nil
}

// setupTestBroker creates a Broker with mock providers and a real SQLite DB.
func setupTestBroker(t *testing.T) (*Broker, *mockProvider, *mockProvider) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-broker-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatalf("create axiom dir: %v", err)
	}

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create IPC directories.
	ipcDir := filepath.Join(axiomDir, "containers", "ipc")
	if err := os.MkdirAll(ipcDir, 0755); err != nil {
		t.Fatalf("create ipc dir: %v", err)
	}

	emitter := events.NewEmitter()
	writer := ipc.NewWriter(ipcDir)

	openrouter := &mockProvider{name: "openrouter", available: true}
	bitnet := &mockProvider{name: "bitnet", available: true}

	broker := New(openrouter, bitnet, db, emitter, writer, Config{
		BudgetMaxUSD:  10.0,
		MaxReqPerTask: 50,
	})

	// Register test models.
	broker.RegisterModel(&ModelInfo{
		ID: "anthropic/claude-4-sonnet", Tier: TierStandard, Source: "openrouter",
		Pricing: ModelPricing{PromptPerMillion: 3.0, CompletionPerMillion: 15.0},
	})
	broker.RegisterModel(&ModelInfo{
		ID: "openai/gpt-4o", Tier: TierStandard, Source: "openrouter",
		Pricing: ModelPricing{PromptPerMillion: 2.5, CompletionPerMillion: 10.0},
	})
	broker.RegisterModel(&ModelInfo{
		ID: "falcon3-1b", Tier: TierLocal, Source: "bitnet",
		Pricing: ModelPricing{PromptPerMillion: 0, CompletionPerMillion: 0},
	})
	broker.RegisterModel(&ModelInfo{
		ID: "anthropic/claude-4-opus", Tier: TierPremium, Source: "openrouter",
		Pricing: ModelPricing{PromptPerMillion: 15.0, CompletionPerMillion: 75.0},
	})

	return broker, openrouter, bitnet
}

// createTestTask is a helper to create a task in the DB for testing.
func createTestTask(t *testing.T, b *Broker, taskID, tier string) {
	t.Helper()
	err := b.db.CreateTask(&state.Task{
		ID: taskID, Title: "Test", Status: "queued", Tier: tier, TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task %s: %v", taskID, err)
	}
	b.SetTaskTier(taskID, ModelTier(tier))
}

func TestBasicInferenceRouting(t *testing.T) {
	b, openrouter, _ := setupTestBroker(t)
	createTestTask(t, b, "task-route", "standard")

	req := &InferenceRequest{
		TaskID:    "task-route",
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	resp, err := b.route(context.Background(), req)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("content = %s, want 'mock response'", resp.Content)
	}
	if openrouter.callCount != 1 {
		t.Errorf("openrouter call count = %d, want 1", openrouter.callCount)
	}
}

func TestBitNetRouting(t *testing.T) {
	b, openrouter, bitnet := setupTestBroker(t)
	createTestTask(t, b, "task-bitnet", "local")

	req := &InferenceRequest{
		TaskID:    "task-bitnet",
		ModelID:   "falcon3-1b",
		Messages:  []ChatMessage{{Role: "user", Content: "rename var"}},
		MaxTokens: 100,
	}

	resp, err := b.route(context.Background(), req)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("content = %s", resp.Content)
	}
	if bitnet.callCount != 1 {
		t.Errorf("bitnet call count = %d, want 1", bitnet.callCount)
	}
	if openrouter.callCount != 0 {
		t.Errorf("openrouter should not have been called")
	}
}

func TestModelAllowlistEnforcement(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	createTestTask(t, b, "task-allowlist", "local")

	// A local-tier task trying to use a premium model should fail.
	req := &InferenceRequest{
		TaskID:    "task-allowlist",
		ModelID:   "anthropic/claude-4-opus",
		Messages:  []ChatMessage{{Role: "user", Content: "complex task"}},
		MaxTokens: 1000,
	}

	err := b.validate(req)
	if err == nil {
		t.Error("expected allowlist error for local task using premium model")
	}
}

func TestModelAllowlistAllowed(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	createTestTask(t, b, "task-allowed", "standard")

	// A standard-tier task using a standard model should pass.
	req := &InferenceRequest{
		TaskID:    "task-allowed",
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	err := b.validate(req)
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestBudgetEnforcement(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	b.config.BudgetMaxUSD = 0.001 // Very small budget
	createTestTask(t, b, "task-budget", "premium")

	// A request with max_tokens that would exceed the tiny budget.
	req := &InferenceRequest{
		TaskID:    "task-budget",
		ModelID:   "anthropic/claude-4-opus",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 10000, // 10000 * 75/1M = $0.75 > $0.001
	}

	err := b.validate(req)
	if err == nil {
		t.Error("expected budget error")
	}
}

func TestRateLimitEnforcement(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	b.config.MaxReqPerTask = 3
	createTestTask(t, b, "task-ratelimit", "standard")

	// Simulate 3 prior requests.
	b.mu.Lock()
	b.taskReqCount["task-ratelimit"] = 3
	b.mu.Unlock()

	req := &InferenceRequest{
		TaskID:    "task-ratelimit",
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	err := b.validate(req)
	if err == nil {
		t.Error("expected rate limit error")
	}
}

func TestResetTaskCount(t *testing.T) {
	b, _, _ := setupTestBroker(t)

	b.mu.Lock()
	b.taskReqCount["task-reset"] = 50
	b.mu.Unlock()

	b.ResetTaskCount("task-reset")

	if b.GetTaskRequestCount("task-reset") != 0 {
		t.Error("expected count to be reset to 0")
	}
}

func TestCostLogging(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	createTestTask(t, b, "task-cost", "standard")

	req := &InferenceRequest{
		TaskID:    "task-cost",
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	resp := &InferenceResponse{
		Content:      "response",
		ModelID:      "anthropic/claude-4-sonnet",
		InputTokens:  100,
		OutputTokens: 50,
		FinishReason: "stop",
		Latency:      500 * time.Millisecond,
	}

	b.logCost(req, resp)

	// Verify cost was logged in SQLite.
	cost, err := b.db.GetTaskCost("task-cost")
	if err != nil {
		t.Fatalf("get task cost: %v", err)
	}
	// Expected: 100 * 3.0/1M + 50 * 15.0/1M = 0.0003 + 0.00075 = 0.00105
	if cost < 0.001 || cost > 0.002 {
		t.Errorf("unexpected cost: $%.6f (expected ~$0.00105)", cost)
	}
}

func TestFallbackToLocalOnOpenRouterDown(t *testing.T) {
	b, openrouter, bitnet := setupTestBroker(t)
	createTestTask(t, b, "task-fallback", "local")

	// Mark OpenRouter as unavailable.
	openrouter.available = false

	req := &InferenceRequest{
		TaskID:    "task-fallback",
		ModelID:   "falcon3-1b",
		Messages:  []ChatMessage{{Role: "user", Content: "rename"}},
		MaxTokens: 50,
	}

	resp, err := b.route(context.Background(), req)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response from BitNet fallback")
	}
	if bitnet.callCount != 1 {
		t.Errorf("bitnet call count = %d, want 1", bitnet.callCount)
	}
}

func TestProviderUnavailableEvent(t *testing.T) {
	b, openrouter, bitnet := setupTestBroker(t)
	createTestTask(t, b, "task-unavail", "standard")

	// Mark both providers as unavailable.
	openrouter.available = false
	bitnet.available = false

	var eventEmitted bool
	b.emitter.Subscribe(events.EventProviderUnavailable, func(e events.Event) {
		eventEmitted = true
	})

	req := &InferenceRequest{
		TaskID:    "task-unavail",
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	provider := b.selectProvider(req)
	if provider != nil {
		t.Error("expected nil provider when all unavailable")
	}

	// Wait for async event.
	time.Sleep(100 * time.Millisecond)
	if !eventEmitted {
		t.Error("expected provider_unavailable event")
	}
}

func TestHandleInferenceRequest(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	createTestTask(t, b, "task-handle", "standard")

	// Create IPC input dir for response writing.
	ipcDir := filepath.Join(os.TempDir(), "axiom-broker-handle-test")
	os.MkdirAll(filepath.Join(ipcDir, "task-handle", "input"), 0755)
	defer os.RemoveAll(ipcDir)

	// Build an IPC inference request message.
	reqMsg := &ipc.InferenceRequestMessage{
		Header:    ipc.Header{Type: ipc.TypeInferenceRequest, TaskID: "task-handle"},
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ipc.ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	// Call the handler.
	resp, err := b.HandleInferenceRequest("task-handle", reqMsg, raw)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	respMsg, ok := resp.(*ipc.InferenceResponseMessage)
	if !ok {
		t.Fatalf("expected *InferenceResponseMessage, got %T", resp)
	}
	if respMsg.FinishReason == "error" {
		t.Errorf("got error response: %s", respMsg.Error)
	}
	if respMsg.Content != "mock response" {
		t.Errorf("content = %s, want 'mock response'", respMsg.Content)
	}

	// Verify request count was incremented.
	if b.GetTaskRequestCount("task-handle") != 1 {
		t.Errorf("request count = %d, want 1", b.GetTaskRequestCount("task-handle"))
	}
}

func TestHandleInferenceRequestValidationFailure(t *testing.T) {
	b, _, _ := setupTestBroker(t)
	createTestTask(t, b, "task-valfail", "local")

	// Local-tier task trying premium model.
	reqMsg := &ipc.InferenceRequestMessage{
		Header:    ipc.Header{Type: ipc.TypeInferenceRequest, TaskID: "task-valfail"},
		ModelID:   "anthropic/claude-4-opus",
		Messages:  []ipc.ChatMessage{{Role: "user", Content: "complex"}},
		MaxTokens: 1000,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, err := b.HandleInferenceRequest("task-valfail", reqMsg, raw)
	if err != nil {
		t.Fatalf("handle should not return Go error: %v", err)
	}

	respMsg, ok := resp.(*ipc.InferenceResponseMessage)
	if !ok {
		t.Fatalf("expected *InferenceResponseMessage, got %T", resp)
	}
	if respMsg.FinishReason != "error" {
		t.Error("expected error finish_reason for validation failure")
	}
	if respMsg.Error == "" {
		t.Error("expected error message in response")
	}
}

func TestProviderError(t *testing.T) {
	b, openrouter, _ := setupTestBroker(t)
	createTestTask(t, b, "task-err", "standard")

	openrouter.err = fmt.Errorf("connection refused")

	reqMsg := &ipc.InferenceRequestMessage{
		Header:    ipc.Header{Type: ipc.TypeInferenceRequest, TaskID: "task-err"},
		ModelID:   "anthropic/claude-4-sonnet",
		Messages:  []ipc.ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := b.HandleInferenceRequest("task-err", reqMsg, raw)
	respMsg := resp.(*ipc.InferenceResponseMessage)
	if respMsg.FinishReason != "error" {
		t.Error("expected error response when provider fails")
	}
}
