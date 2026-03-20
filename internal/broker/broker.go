// Package broker implements the Inference Broker, the engine component that
// mediates ALL model API calls from containers. No container ever calls a
// model API directly.
//
// The broker validates every request against:
//   - Model allowlist (task tier determines allowed model tiers)
//   - Token budget (max_tokens * pricing must fit remaining budget)
//   - Rate limits (per-task request count, default 50)
//
// See Architecture.md Section 19.5 for the full Inference Broker specification.
package broker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

// ModelTier represents the capability/cost tier of a model.
// See Architecture Section 10.2.
type ModelTier string

const (
	TierLocal    ModelTier = "local"
	TierCheap    ModelTier = "cheap"
	TierStandard ModelTier = "standard"
	TierPremium  ModelTier = "premium"
)

// tierAllowlist maps a task tier to the model tiers it may use.
// A local-tier task cannot request a premium model.
// See Architecture Section 19.5 (Per-Task Enforcement, point 1).
var tierAllowlist = map[ModelTier]map[ModelTier]bool{
	TierLocal:    {TierLocal: true},
	TierCheap:    {TierLocal: true, TierCheap: true},
	TierStandard: {TierLocal: true, TierCheap: true, TierStandard: true},
	TierPremium:  {TierLocal: true, TierCheap: true, TierStandard: true, TierPremium: true},
}

// ModelPricing holds per-token pricing for a model.
type ModelPricing struct {
	PromptPerMillion     float64 // USD per million input tokens
	CompletionPerMillion float64 // USD per million output tokens
}

// ModelInfo holds metadata about a registered model.
type ModelInfo struct {
	ID      string
	Tier    ModelTier
	Pricing ModelPricing
	Source  string // "openrouter" | "bitnet"
}

// Provider is the interface for model inference backends.
// Both OpenRouter and BitNet implement this interface.
type Provider interface {
	// Complete sends a non-streaming completion request.
	Complete(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error)

	// CompleteStream sends a streaming completion request. The onChunk
	// callback is called for each partial response. The final InferenceResponse
	// contains the aggregated token counts.
	CompleteStream(ctx context.Context, req *InferenceRequest, onChunk func(chunk string, done bool)) (*InferenceResponse, error)

	// Name returns the provider name (e.g. "openrouter", "bitnet").
	Name() string

	// Available checks if the provider is reachable.
	Available(ctx context.Context) bool
}

// InferenceRequest is the broker's internal representation of an inference request.
type InferenceRequest struct {
	TaskID             string
	ModelID            string
	Messages           []ChatMessage
	MaxTokens          int
	Temperature        float64
	GrammarConstraints *string // GBNF grammar for BitNet, nil otherwise
	AgentType          string  // "meeseeks" | "reviewer" | "sub_orchestrator" | "orchestrator"
}

// ChatMessage represents a single message in a chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// InferenceResponse is the broker's internal representation of an inference response.
type InferenceResponse struct {
	Content      string
	ModelID      string
	InputTokens  int
	OutputTokens int
	FinishReason string // "stop" | "length" | "error"
	Latency      time.Duration
}

// Config holds Broker configuration.
type Config struct {
	BudgetMaxUSD  float64 // Project budget ceiling
	MaxReqPerTask int     // Max inference requests per task (default 50)
	IPCBaseDir    string  // Base directory for IPC (e.g. ".axiom/containers/ipc")
}

// Broker mediates all model API calls. Containers submit inference requests
// via IPC; the broker validates, routes, executes, and logs them.
// See Architecture Section 19.5.
type Broker struct {
	openrouter Provider
	bitnet     Provider
	db         *state.DB
	emitter    *events.Emitter
	ipcWriter  *ipc.Writer
	config     Config

	mu           sync.Mutex
	taskReqCount map[string]int            // task_id -> request count for rate limiting
	models       map[string]*ModelInfo     // model_id -> info
	taskTiers    map[string]ModelTier      // task_id -> assigned tier
}

// New creates a new Broker with the given providers and configuration.
func New(openrouter, bitnet Provider, db *state.DB, emitter *events.Emitter, ipcWriter *ipc.Writer, config Config) *Broker {
	if config.MaxReqPerTask == 0 {
		config.MaxReqPerTask = 50
	}
	return &Broker{
		openrouter:   openrouter,
		bitnet:       bitnet,
		db:           db,
		emitter:      emitter,
		ipcWriter:    ipcWriter,
		config:       config,
		taskReqCount: make(map[string]int),
		models:       make(map[string]*ModelInfo),
		taskTiers:    make(map[string]ModelTier),
	}
}

// RegisterModel adds a model to the broker's registry with its tier and pricing.
func (b *Broker) RegisterModel(info *ModelInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.models[info.ID] = info
}

// SetTaskTier records the model tier assigned to a task.
// This is set by the orchestrator when building the TaskSpec.
func (b *Broker) SetTaskTier(taskID string, tier ModelTier) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.taskTiers[taskID] = tier
}

// HandleInferenceRequest processes an inference_request IPC message.
// This is the main entry point, designed to be registered as a handler
// with the IPC Dispatcher.
//
// It validates the request, routes to the appropriate provider, logs costs,
// and returns the response. For streaming requests, it writes chunked IPC
// files via the StreamWriter.
func (b *Broker) HandleInferenceRequest(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	reqMsg, ok := msg.(*ipc.InferenceRequestMessage)
	if !ok {
		return nil, fmt.Errorf("expected InferenceRequestMessage, got %T", msg)
	}

	// Convert IPC message to internal request.
	req := &InferenceRequest{
		TaskID:    taskID,
		ModelID:   reqMsg.ModelID,
		MaxTokens: reqMsg.MaxTokens,
		Temperature: reqMsg.Temperature,
		GrammarConstraints: reqMsg.GrammarConstraints,
	}
	for _, m := range reqMsg.Messages {
		req.Messages = append(req.Messages, ChatMessage{Role: m.Role, Content: m.Content})
	}

	// Run all validations.
	if err := b.validate(req); err != nil {
		return &ipc.InferenceResponseMessage{
			Header:       ipc.Header{Type: ipc.TypeInferenceResponse, TaskID: taskID},
			ModelID:      req.ModelID,
			FinishReason: "error",
			Error:        err.Error(),
		}, nil
	}

	// Route and execute.
	start := time.Now()
	resp, err := b.route(context.Background(), req)
	if err != nil {
		return &ipc.InferenceResponseMessage{
			Header:       ipc.Header{Type: ipc.TypeInferenceResponse, TaskID: taskID},
			ModelID:      req.ModelID,
			FinishReason: "error",
			Error:        err.Error(),
		}, nil
	}
	resp.Latency = time.Since(start)

	// Log to cost_log.
	b.logCost(req, resp)

	// Increment request count for rate limiting.
	b.mu.Lock()
	b.taskReqCount[taskID]++
	b.mu.Unlock()

	// Build IPC response.
	return &ipc.InferenceResponseMessage{
		Header:       ipc.Header{Type: ipc.TypeInferenceResponse, TaskID: taskID},
		ModelID:      resp.ModelID,
		Content:      resp.Content,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		FinishReason: resp.FinishReason,
	}, nil
}

// HandleStreamingInferenceRequest handles a streaming inference request,
// writing chunked response files to the container's IPC input directory.
// See Architecture Section 19.5 (Streaming).
func (b *Broker) HandleStreamingInferenceRequest(taskID string, req *InferenceRequest) error {
	if err := b.validate(req); err != nil {
		return err
	}

	provider := b.selectProvider(req)
	if provider == nil {
		return fmt.Errorf("no available provider for model %s", req.ModelID)
	}

	sw := ipc.NewStreamWriter(b.ipcWriterBaseDir(), taskID)

	start := time.Now()
	resp, err := provider.CompleteStream(context.Background(), req, func(chunk string, done bool) {
		streamChunk := &ipc.InferenceStreamChunk{
			Header:  ipc.Header{Type: ipc.TypeInferenceResponse, TaskID: taskID},
			Content: chunk,
			Done:    done,
		}
		_ = sw.WriteChunk(streamChunk)
	})
	if err != nil {
		return fmt.Errorf("streaming completion: %w", err)
	}
	resp.Latency = time.Since(start)

	b.logCost(req, resp)

	b.mu.Lock()
	b.taskReqCount[taskID]++
	b.mu.Unlock()

	return nil
}

// validate runs all pre-request checks: model allowlist, budget, and rate limit.
func (b *Broker) validate(req *InferenceRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 1. Model allowlist check.
	// See Architecture Section 19.5 (Per-Task Enforcement, point 1).
	taskTier, tierKnown := b.taskTiers[req.TaskID]
	modelInfo, modelKnown := b.models[req.ModelID]

	if tierKnown && modelKnown {
		allowed := tierAllowlist[taskTier]
		if allowed != nil && !allowed[modelInfo.Tier] {
			return fmt.Errorf("model %s (tier %s) not allowed for task tier %s",
				req.ModelID, modelInfo.Tier, taskTier)
		}
	}

	// 2. Budget check.
	// See Architecture Section 19.5 (Per-Task Enforcement, point 2).
	if modelKnown && b.config.BudgetMaxUSD > 0 {
		maxCost := float64(req.MaxTokens) * modelInfo.Pricing.CompletionPerMillion / 1_000_000
		currentSpend, err := b.db.GetProjectCost()
		if err == nil {
			remaining := b.config.BudgetMaxUSD - currentSpend
			if maxCost > remaining {
				return fmt.Errorf("budget exceeded: request max cost $%.4f, remaining $%.4f",
					maxCost, remaining)
			}
		}
	}

	// 3. Rate limit check.
	// See Architecture Section 19.5 (Per-Task Enforcement, point 3).
	count := b.taskReqCount[req.TaskID]
	if count >= b.config.MaxReqPerTask {
		return fmt.Errorf("rate limit exceeded: task %s has made %d/%d requests",
			req.TaskID, count, b.config.MaxReqPerTask)
	}

	return nil
}

// route selects the appropriate provider and executes the request.
// Implements fallback logic: if OpenRouter is unavailable, BitNet-eligible
// tasks auto-route to local. See Architecture Section 19.5 (Fallback Behavior).
func (b *Broker) route(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	provider := b.selectProvider(req)
	if provider == nil {
		return nil, fmt.Errorf("no available provider for model %s", req.ModelID)
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", provider.Name(), err)
	}

	return resp, nil
}

// selectProvider determines which provider should handle the request.
func (b *Broker) selectProvider(req *InferenceRequest) Provider {
	b.mu.Lock()
	modelInfo, known := b.models[req.ModelID]
	b.mu.Unlock()

	// If the model is a local/BitNet model, use BitNet.
	if known && modelInfo.Source == "bitnet" {
		if b.bitnet != nil && b.bitnet.Available(context.Background()) {
			return b.bitnet
		}
		return nil
	}

	// Try OpenRouter first.
	if b.openrouter != nil && b.openrouter.Available(context.Background()) {
		return b.openrouter
	}

	// Fallback: if OpenRouter is down, BitNet-eligible tasks route to local.
	if known && modelInfo.Tier == TierLocal {
		if b.bitnet != nil && b.bitnet.Available(context.Background()) {
			b.emitter.Emit(events.Event{
				Type:   events.EventProviderUnavailable,
				TaskID: req.TaskID,
				Details: map[string]interface{}{
					"provider":  "openrouter",
					"fallback":  "bitnet",
					"model_id":  req.ModelID,
				},
			})
			return b.bitnet
		}
	}

	// No provider available.
	b.emitter.Emit(events.Event{
		Type:   events.EventProviderUnavailable,
		TaskID: req.TaskID,
		Details: map[string]interface{}{
			"provider": "all",
			"model_id": req.ModelID,
		},
	})
	return nil
}

// logCost records the inference cost in the cost_log table.
// See Architecture Section 19.5 (Audit).
func (b *Broker) logCost(req *InferenceRequest, resp *InferenceResponse) {
	b.mu.Lock()
	modelInfo, known := b.models[req.ModelID]
	b.mu.Unlock()

	costUSD := 0.0
	if known {
		costUSD = float64(resp.InputTokens)*modelInfo.Pricing.PromptPerMillion/1_000_000 +
			float64(resp.OutputTokens)*modelInfo.Pricing.CompletionPerMillion/1_000_000
	}

	agentType := req.AgentType
	if agentType == "" {
		agentType = "meeseeks" // default for backward compatibility
	}

	_ = b.db.InsertCost(&state.CostEntry{
		TaskID:       req.TaskID,
		AgentType:    agentType,
		ModelID:      req.ModelID,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      costUSD,
		Timestamp:    time.Now(),
	})
}

// ipcWriterBaseDir returns the base IPC directory for the StreamWriter.
// Uses the IPCBaseDir from the broker config. Falls back to the IPC writer's
// base directory if no explicit config value is set.
func (b *Broker) ipcWriterBaseDir() string {
	if b.config.IPCBaseDir != "" {
		return b.config.IPCBaseDir
	}
	// Fallback: derive from the IPC writer's base directory.
	return b.ipcWriter.BaseDir()
}

// ResetTaskCount resets the rate limit counter for a task.
// Called when a task gets a fresh Meeseeks container (retry/escalation).
func (b *Broker) ResetTaskCount(taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.taskReqCount, taskID)
}

// GetTaskRequestCount returns the current request count for a task.
func (b *Broker) GetTaskRequestCount(taskID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.taskReqCount[taskID]
}

// RouteRequest provides direct access to the inference broker for in-process
// callers (e.g., the DirectOrchestrator). Unlike HandleInferenceRequest, this
// accepts an InferenceRequest directly rather than an IPC message.
// Cost is logged and rate-limited the same way as IPC-based requests.
func (b *Broker) RouteRequest(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	start := time.Now()
	resp, err := b.route(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Latency = time.Since(start)

	b.logCost(req, resp)

	b.mu.Lock()
	b.taskReqCount[req.TaskID]++
	b.mu.Unlock()

	return resp, nil
}

// UpdateOpenRouterKey updates the OpenRouter API key at runtime.
// This supports config reload without engine restart per Architecture Section 19.5.
func (b *Broker) UpdateOpenRouterKey(key string) {
	if orClient, ok := b.openrouter.(*OpenRouterClient); ok {
		orClient.UpdateAPIKey(key)
	}
}
