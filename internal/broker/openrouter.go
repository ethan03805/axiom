package broker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
		timeout = 300 * time.Second
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

// maxRetries is the maximum number of retry attempts for transient errors.
const maxRetries = 3

// retryBackoffs defines the exponential backoff durations for each retry attempt.
var retryBackoffs = [maxRetries]time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// isRetryable returns true if the HTTP status code warrants a retry.
// Only 429 (rate limit) and 5xx (server error) responses are retried.
// Other 4xx errors are NOT retried as they indicate client-side issues.
func isRetryable(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

// retryDelay computes the delay before the next retry attempt. If the response
// includes a Retry-After header (typically on 429 responses), that value is
// used instead of the default exponential backoff.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return retryBackoffs[attempt]
}

// doWithRetry executes an HTTP request with retry logic for transient errors.
// It retries on 429 (rate limit) and 5xx (server error) responses up to
// maxRetries times with exponential backoff (1s, 2s, 4s). The Retry-After
// header is honoured on 429 responses. Non-retryable 4xx errors are returned
// immediately.
//
// The buildReq function is called for every attempt so that the request body
// reader is fresh (http.Request bodies are consumed on send).
func (c *OpenRouterClient) doWithRetry(ctx context.Context, buildReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		httpReq, err := buildReq()
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			// Network errors are transient; sleep and retry.
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryBackoffs[attempt]):
				}
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		// Read the error body for the error message.
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("openrouter error %d: %s", resp.StatusCode, string(bodyBytes))

		if !isRetryable(resp.StatusCode) || attempt == maxRetries {
			return nil, lastErr
		}

		// Wait before retrying with exponential backoff or Retry-After.
		delay := retryDelay(resp, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

// Complete sends a non-streaming completion request to OpenRouter.
func (c *OpenRouterClient) Complete(ctx context.Context, req *InferenceRequest) (*InferenceResponse, error) {
	body := c.buildRequestBody(req, false)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	buildReq := func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		return httpReq, nil
	}

	resp, err := c.doWithRetry(ctx, buildReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

	buildReq := func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		return httpReq, nil
	}

	resp, err := c.doWithRetry(ctx, buildReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
