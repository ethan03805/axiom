package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BitNetClient implements the Provider interface for the local BitNet
// inference server. BitNet provides free, zero-latency local inference
// for trivial tasks using 1-bit quantized models (Falcon3).
//
// The BitNet server exposes an OpenAI-compatible API at localhost:3002.
// See Architecture Section 19.
type BitNetClient struct {
	baseURL    string
	httpClient *http.Client
}

// BitNetConfig holds configuration for the BitNet client.
type BitNetConfig struct {
	Host    string
	Port    int
	Timeout time.Duration
}

// NewBitNetClient creates a BitNet provider client.
func NewBitNetClient(config BitNetConfig) *BitNetClient {
	host := config.Host
	if host == "" {
		host = "localhost"
	}
	port := config.Port
	if port == 0 {
		port = 3002
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &BitNetClient{
		baseURL: fmt.Sprintf("http://%s:%d/v1", host, port),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *BitNetClient) Name() string { return "bitnet" }

// Available checks if the BitNet server is reachable by sending a
// lightweight request. Returns false if the server is not running.
func (c *BitNetClient) Available(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Complete sends a non-streaming completion request to the local BitNet server.
// Supports grammar-constrained decoding via the grammar_constraints field.
// See Architecture Section 19.3.
func (c *BitNetClient) Complete(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	body := c.buildRequestBody(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bitnet request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bitnet error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return c.parseResponse(resp.Body, req.ModelID)
}

// CompleteStream sends a streaming request to BitNet. BitNet's streaming
// support uses the same SSE format as OpenAI/OpenRouter.
func (c *BitNetClient) CompleteStream(ctx context.Context, req *InferenceRequest, onChunk func(chunk string, done bool)) (*InferenceResponse, error) {
	body := c.buildRequestBody(req)
	body["stream"] = true
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bitnet stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bitnet error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return parseSSEStream(resp.Body, req.ModelID, onChunk)
}

// bitnetRequest is the request body for the BitNet OpenAI-compatible API.
// Includes the grammar field for grammar-constrained decoding.
type bitnetRequest map[string]interface{}

func (c *BitNetClient) buildRequestBody(req *InferenceRequest) bitnetRequest {
	msgs := make([]map[string]string, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = map[string]string{"role": m.Role, "content": m.Content}
	}

	body := bitnetRequest{
		"model":       req.ModelID,
		"messages":    msgs,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
	}

	// Include grammar constraints if present.
	// See Architecture Section 19.3 (Grammar-Constrained Decoding).
	if req.GrammarConstraints != nil {
		body["grammar"] = *req.GrammarConstraints
	}

	return body
}

func (c *BitNetClient) parseResponse(body io.Reader, modelID string) (*InferenceResponse, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	finishReason := "stop"
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		if resp.Choices[0].FinishReason != "" {
			finishReason = resp.Choices[0].FinishReason
		}
	}

	return &InferenceResponse{
		Content:      content,
		ModelID:      modelID,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		FinishReason: finishReason,
	}, nil
}
