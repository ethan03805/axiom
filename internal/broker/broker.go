package broker

import (
	"context"
)

// ModelProvider defines the interface for LLM API providers.
type ModelProvider interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	Name() string
	Models() []string
}

// CompletionRequest holds parameters for an LLM completion request.
type CompletionRequest struct {
	Model       string
	Messages    []ChatMessage
	MaxTokens   int
	Temperature float64
	Stop        []string
}

// ChatMessage represents a single message in a chat completion.
type ChatMessage struct {
	Role    string
	Content string
}

// CompletionResponse holds the result of an LLM completion.
type CompletionResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	FinishReason string
}

// Broker routes LLM requests to the appropriate provider based on
// model selection, cost, and availability.
type Broker struct {
	providers map[string]ModelProvider
}

// New creates a new Broker.
func New() *Broker {
	return &Broker{
		providers: make(map[string]ModelProvider),
	}
}

// RegisterProvider adds a model provider to the broker.
func (b *Broker) RegisterProvider(name string, provider ModelProvider) {
	b.providers[name] = provider
}

// Complete sends a completion request, routing to the appropriate provider.
func (b *Broker) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	return nil, nil
}

// EstimateCost estimates the cost for a given request without executing it.
func (b *Broker) EstimateCost(req *CompletionRequest) (float64, error) {
	return 0, nil
}
