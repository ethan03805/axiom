package broker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouterClient implements the Provider interface for the OpenRouter API.
// OpenRouter provides a unified API for accessing multiple model providers.
// See Architecture Section 19.5.
type OpenRouterClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// OpenRouterConfig holds configuration for the OpenRouter client.
type OpenRouterConfig struct {
	APIKey  string
	BaseURL string // Defaults to "https://openrouter.ai/api/v1"
	Timeout time.Duration
}

// NewOpenRouterClient creates an OpenRouter provider client.
// API key is loaded from engine config, never injected into containers.
// See Architecture Section 19.5 (Credential Management).
func NewOpenRouterClient(config OpenRouterConfig) *OpenRouterClient {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &OpenRouterClient{
		apiKey:  config.APIKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *OpenRouterClient) Name() string { return "openrouter" }

// Available checks if the OpenRouter API is reachable by verifying the
// API key is configured. A full health check would hit /api/v1/models
// but that is deferred to avoid unnecessary API calls.
func (c *OpenRouterClient) Available(_ context.Context) bool {
	return c.apiKey != ""
}

// Complete sends a non-streaming completion request to OpenRouter.
func (c *OpenRouterClient) Complete(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	body := c.buildRequestBody(req, false)
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
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return c.parseResponse(resp.Body, req.ModelID)
}

// CompleteStream sends a streaming completion request to OpenRouter.
// The onChunk callback is called for each SSE data event.
// See Architecture Section 19.5 (Streaming).
func (c *OpenRouterClient) CompleteStream(ctx context.Context, req *InferenceRequest, onChunk func(chunk string, done bool)) (*InferenceResponse, error) {
	body := c.buildRequestBody(req, true)
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
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return parseSSEStream(resp.Body, req.ModelID, onChunk)
}

// UpdateAPIKey updates the API key. Supports config reload without restart.
// See Architecture Section 19.5 (Credential Management).
func (c *OpenRouterClient) UpdateAPIKey(key string) {
	c.apiKey = key
}

// openRouterRequest is the request body for the OpenRouter chat completions API.
type openRouterRequest struct {
	Model       string             `json:"model"`
	Messages    []openRouterMsg    `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type openRouterMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openRouterResponse is the response from the OpenRouter chat completions API.
type openRouterResponse struct {
	ID      string `json:"id"`
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

func (c *OpenRouterClient) buildRequestBody(req *InferenceRequest, stream bool) openRouterRequest {
	msgs := make([]openRouterMsg, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openRouterMsg{Role: m.Role, Content: m.Content}
	}
	return openRouterRequest{
		Model:       req.ModelID,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}
}

func (c *OpenRouterClient) parseResponse(body io.Reader, modelID string) (*InferenceResponse, error) {
	var resp openRouterResponse
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

// parseSSEStream reads an SSE stream and calls onChunk for each data event.
// Returns the final aggregated response with token counts.
func parseSSEStream(body io.Reader, modelID string, onChunk func(chunk string, done bool)) (*InferenceResponse, error) {
	scanner := bufio.NewScanner(body)
	var fullContent strings.Builder
	var inputTokens, outputTokens int
	finishReason := "stop"

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}" or "data: [DONE]"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			onChunk("", true)
			break
		}

		// Parse the SSE chunk.
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				fullContent.WriteString(delta)
				onChunk(delta, false)
			}
			if chunk.Choices[0].FinishReason != nil {
				finishReason = *chunk.Choices[0].FinishReason
			}
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}
	}

	return &InferenceResponse{
		Content:      fullContent.String(),
		ModelID:      modelID,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		FinishReason: finishReason,
	}, scanner.Err()
}
