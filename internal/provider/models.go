package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/pkg/ai"
)

// Model represents a model returned by the models.dev catalog, optionally
// merged with a provider's manually-configured models.
type Model struct {
	ID     string             `json:"id"`
	Name   string             `json:"name,omitempty"`
	URL    string             `json:"url"`
	Ready  bool               `json:"ready"`
	Config config.ModelConfig `json:"-"`
}

// DiscoverModels returns available models for a provider from the
// models.dev catalog, merged with any models declared directly in the tau
// configuration. It no longer calls the provider's /v1/models endpoint.
func DiscoverModels(ctx context.Context, provider config.ProviderConfig, bearerToken string, insecure bool) ([]Model, error) {
	_ = bearerToken // Legacy parameter; kept for API compatibility.

	catalog := ai.NewCatalog(ai.DefaultCatalogOptions(insecure))
	if err := catalog.Load(ctx); err != nil {
		// If the catalog is unavailable, fall back to configured models only.
		return ConfiguredModels(provider), nil
	}

	return mergeCatalogWithConfigured(catalog, provider), nil
}

// RefreshModels forces a network refresh of the catalog before resolving
// models for the provider.
func RefreshModels(ctx context.Context, provider config.ProviderConfig, insecure bool) ([]Model, error) {
	catalog := ai.NewCatalog(ai.DefaultCatalogOptions(insecure))
	if err := catalog.Fetch(ctx); err != nil {
		return nil, fmt.Errorf("refreshing model catalog: %w", err)
	}
	return mergeCatalogWithConfigured(catalog, provider), nil
}

// ConfiguredModels returns ready model refs declared directly in provider
// config.
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

func mergeCatalogWithConfigured(catalog *ai.Catalog, provider config.ProviderConfig) []Model {
	configured := ConfiguredModels(provider)
	models := make([]Model, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))

	for _, m := range configured {
		models = append(models, m)
		seen[m.ID] = struct{}{}
	}

	catalogModels, err := catalog.Models(provider.Name)
	if err != nil {
		return models
	}

	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if apiURL, ok := catalog.API(provider.Name); ok && baseURL == "" {
		baseURL = apiURL
	}

	for _, cm := range catalogModels {
		if _, exists := seen[cm.ID]; exists {
			continue
		}
		models = append(models, Model{
			ID:    cm.ID,
			Name:  cm.Name,
			URL:   baseURL,
			Ready: true,
			Config: config.ModelConfig{
				ID:               cm.ID,
				Name:             cm.Name,
				ContextWindow:    cm.Context,
				DefaultMaxTokens: cm.Output,
				Reasoning:        cm.Reasoning,
				Cost: config.CostConfig{
					Input:      cm.Cost.Input,
					Output:     cm.Cost.Output,
					CacheRead:  cm.Cost.CacheRead,
					CacheWrite: cm.Cost.CacheWrite,
				},
			},
		})
		seen[cm.ID] = struct{}{}
	}

	return models
}
