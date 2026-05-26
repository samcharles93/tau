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
	ID    string `json:"id"`
	URL   string `json:"url"`
	Ready bool   `json:"ready"`
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
	url := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	client := platform.NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model discovery: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading model discovery response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("provider %q bearer token was rejected (401)", provider.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model discovery returned %d: %s", resp.StatusCode, body)
	}

	var response modelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid JSON from models endpoint: %w", err)
	}
	models := make([]Model, 0, len(response.Data))
	for _, item := range response.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		models = append(models, Model{ID: item.ID, URL: baseURL, Ready: true})
	}
	return models, nil
}
