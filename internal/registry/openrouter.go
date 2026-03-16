package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouterFetcher fetches model information from the OpenRouter API.
// See Architecture Section 18.2.
type OpenRouterFetcher struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewOpenRouterFetcher creates a fetcher for the OpenRouter models API.
func NewOpenRouterFetcher(apiKey string) *OpenRouterFetcher {
	return &OpenRouterFetcher{
		apiKey:  apiKey,
		baseURL: "https://openrouter.ai/api/v1",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// openRouterModelsResponse is the response from /api/v1/models.
type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Pricing       struct {
		Prompt     string `json:"prompt"`     // Price per token as string
		Completion string `json:"completion"` // Price per token as string
	} `json:"pricing"`
	TopProvider struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

// FetchModels retrieves all available models from OpenRouter.
// Maps each model to a ModelInfo with tier determined by pricing.
// See BUILD_PLAN step 13.2.
func (f *OpenRouterFetcher) FetchModels(ctx context.Context) ([]*ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", f.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if f.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.apiKey)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter API error %d: %s", resp.StatusCode, string(body))
	}

	var result openRouterModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	var models []*ModelInfo
	for _, m := range result.Data {
		info := convertOpenRouterModel(m)
		if info != nil {
			models = append(models, info)
		}
	}
	return models, nil
}

// Available returns true if the fetcher has an API key configured.
func (f *OpenRouterFetcher) Available() bool {
	return f.apiKey != ""
}

// convertOpenRouterModel converts an OpenRouter model to our ModelInfo.
func convertOpenRouterModel(m openRouterModel) *ModelInfo {
	promptPrice := parsePrice(m.Pricing.Prompt)
	completionPrice := parsePrice(m.Pricing.Completion)

	// Determine family from model ID.
	family := extractFamily(m.ID)

	// Determine tier from pricing.
	tier := classifyTier(completionPrice)

	maxOutput := m.TopProvider.MaxCompletionTokens
	if maxOutput == 0 {
		maxOutput = 4096
	}

	return &ModelInfo{
		ID:                   m.ID,
		Family:               family,
		Source:                "openrouter",
		Tier:                 tier,
		ContextWindow:        m.ContextLength,
		MaxOutput:            maxOutput,
		PromptPerMillion:     promptPrice * 1_000_000,
		CompletionPerMillion: completionPrice * 1_000_000,
		SupportsTools:        true, // Most OpenRouter models support tools
		LastUpdated:          time.Now(),
	}
}

// classifyTier determines a model's tier based on completion pricing per token.
// See Architecture Section 10.2 for tier definitions.
func classifyTier(completionPricePerToken float64) ModelTier {
	perMillion := completionPricePerToken * 1_000_000
	switch {
	case perMillion <= 0.5:
		return TierCheap
	case perMillion <= 20:
		return TierStandard
	default:
		return TierPremium
	}
}

// extractFamily determines the model family from the model ID.
func extractFamily(modelID string) string {
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) >= 1 {
		return parts[0]
	}
	return "unknown"
}

// parsePrice parses a price string (per-token) to float64.
// OpenRouter returns prices as strings like "0.000003".
func parsePrice(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// MergeCuratedData overlays curated capability data (strengths, weaknesses,
// recommendations) from a models.json file onto existing registry models.
// See Architecture Section 18.4.
func (r *Registry) MergeCuratedData(curated []*ModelInfo) error {
	for _, c := range curated {
		existing, err := r.Get(c.ID)
		if err != nil {
			continue // Model not in registry; skip
		}

		// Overlay curated fields onto existing model.
		if len(c.Strengths) > 0 {
			existing.Strengths = c.Strengths
		}
		if len(c.Weaknesses) > 0 {
			existing.Weaknesses = c.Weaknesses
		}
		if len(c.RecommendedFor) > 0 {
			existing.RecommendedFor = c.RecommendedFor
		}
		if len(c.NotRecommendedFor) > 0 {
			existing.NotRecommendedFor = c.NotRecommendedFor
		}

		if err := r.Upsert(existing); err != nil {
			return fmt.Errorf("merge curated data for %s: %w", c.ID, err)
		}
	}
	return nil
}
