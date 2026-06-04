package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/platform"
)

// Model represents a model returned by an OpenAI-compatible models endpoint.
type Model struct {
	ID     string             `json:"id"`
	Name   string             `json:"name,omitempty"`
	URL    string             `json:"url"`
	Ready  bool               `json:"ready"`
	Config config.ModelConfig `json:"-"`
}

type modelsResponse struct {
	Data []modelData `json:"data"`
}

type modelData struct {
	ID string `json:"id"`
}

// DiscoverModels fetches available models from GET {base_url}/v1/models.
func DiscoverModels(ctx context.Context, provider config.ProviderConfig, bearerToken string, insecure bool) ([]Model, error) {
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	configured := ConfiguredModels(provider)
	url := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	client := platform.NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		if len(configured) > 0 {
			return configured, nil
		}
		return nil, fmt.Errorf("model discovery: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading model discovery response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		if len(configured) > 0 {
			return configured, nil
		}
		return nil, fmt.Errorf("provider %q bearer token was rejected (401)", provider.Name)
	}
	if resp.StatusCode != http.StatusOK {
		if len(configured) > 0 {
			return configured, nil
		}
		return nil, fmt.Errorf("model discovery returned %d: %s", resp.StatusCode, body)
	}

	var response modelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid JSON from models endpoint: %w", err)
	}
	models := make([]Model, 0, len(configured)+len(response.Data))
	seen := make(map[string]struct{}, len(configured)+len(response.Data))
	for _, model := range configured {
		models = append(models, model)
		seen[model.ID] = struct{}{}
	}
	for _, item := range response.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		models = append(models, Model{ID: item.ID, URL: baseURL, Ready: true})
	}
	return models, nil
}

// ConfiguredModels returns ready model refs declared directly in provider config.
func ConfiguredModels(provider config.ProviderConfig) []Model {
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	models := make([]Model, 0, len(provider.Models))
	for _, cfg := range provider.Models {
		id := strings.TrimSpace(cfg.ID)
		if id == "" {
			continue
		}
		modelURL := baseURL
		if cfg.URL != "" {
			modelURL = strings.TrimRight(cfg.URL, "/")
		}
		models = append(models, Model{
			ID:     id,
			Name:   cfg.Name,
			URL:    modelURL,
			Ready:  true,
			Config: cfg,
		})
	}
	return models
}
