package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

const (
	modelsCatalogURLEnv       = "TAU_MODELS_CATALOG_URL"
	modelsCatalogTTLEnv       = "TAU_MODELS_CATALOG_TTL"
	modelsCatalogCacheFile    = "models.json"
	modelsCatalogOverrideFile = "api.overrides.json"
	defaultCatalogTTL         = 24 * time.Hour
)

var errCatalogUnavailable = errors.New("models catalog unavailable")

type catalogOptions struct {
	URL           string
	CachePath     string
	OverridesPath string
	TTL           time.Duration
	Insecure      bool
}

type modelsCatalog struct {
	providers map[string]catalogProvider
}

type catalogProvider struct {
	ID     string                  `json:"id,omitempty"`
	API    string                  `json:"api,omitempty"`
	Models map[string]catalogModel `json:"models,omitempty"`
}

type catalogModel struct {
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Reasoning bool        `json:"reasoning,omitempty"`
	Context   int         `json:"context,omitempty"`
	Output    int         `json:"output,omitempty"`
	Cost      catalogCost `json:"cost"`
}

type catalogCost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type catalogOverridesFile struct {
	Providers map[string]catalogProvider `json:"providers"`
}

func discoverModelsFromCatalog(ctx context.Context, provider config.ProviderConfig, insecure bool) ([]Model, error) {
	options := defaultCatalogOptions(insecure)
	catalog, err := loadModelsCatalog(ctx, options)
	if err != nil {
		return nil, err
	}
	return catalog.modelsFor(provider)
}

func defaultCatalogOptions(insecure bool) catalogOptions {
	ttl := defaultCatalogTTL
	if raw := strings.TrimSpace(os.Getenv(modelsCatalogTTLEnv)); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			ttl = parsed
		}
	}
	baseDir := config.Dir()
	return catalogOptions{
		URL:           strings.TrimSpace(os.Getenv(modelsCatalogURLEnv)),
		CachePath:     filepath.Join(baseDir, modelsCatalogCacheFile),
		OverridesPath: filepath.Join(baseDir, modelsCatalogOverrideFile),
		TTL:           ttl,
		Insecure:      insecure,
	}
}

func loadModelsCatalog(ctx context.Context, opts catalogOptions) (*modelsCatalog, error) {
	cacheProviders, cacheFresh, cacheErr := readCatalogCache(opts)
	if cacheErr != nil {
		cacheProviders = nil
	}

	providers := cacheProviders
	if providers == nil || !cacheFresh {
		if strings.TrimSpace(opts.URL) != "" {
			fetched, fetchErr := fetchCatalog(ctx, opts)
			if fetchErr == nil {
				providers = fetched
			} else if providers == nil {
				return nil, fmt.Errorf("%w: %v", errCatalogUnavailable, fetchErr)
			}
		} else if providers == nil {
			return nil, errCatalogUnavailable
		}
	}

	if providers == nil {
		return nil, errCatalogUnavailable
	}

	overrides, err := readCatalogOverrides(opts.OverridesPath)
	if err == nil {
		providers = mergeCatalogProviders(providers, overrides)
	}

	return &modelsCatalog{providers: providers}, nil
}

func (c *modelsCatalog) modelsFor(provider config.ProviderConfig) ([]Model, error) {
	if c == nil || len(c.providers) == 0 {
		return nil, errCatalogUnavailable
	}
	key := strings.ToLower(strings.TrimSpace(provider.Name))
	p, ok := c.providers[key]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q not found", errCatalogUnavailable, provider.Name)
	}

	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(p.API, "/")
	}

	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	models := make([]Model, 0, len(ids))
	for _, keyID := range ids {
		entry := p.Models[keyID]
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			id = keyID
		}
		if id == "" {
			continue
		}
		models = append(models, Model{
			ID:    id,
			Name:  entry.Name,
			URL:   baseURL,
			Ready: true,
			Config: config.ModelConfig{
				ID:               id,
				Name:             entry.Name,
				ContextWindow:    entry.Context,
				DefaultMaxTokens: entry.Output,
				Reasoning:        entry.Reasoning,
				Cost: config.CostConfig{
					Input:      entry.Cost.Input,
					Output:     entry.Cost.Output,
					CacheRead:  entry.Cost.CacheRead,
					CacheWrite: entry.Cost.CacheWrite,
				},
			},
		})
	}

	return models, nil
}

func readCatalogCache(opts catalogOptions) (map[string]catalogProvider, bool, error) {
	data, err := os.ReadFile(opts.CachePath)
	if err != nil {
		return nil, false, err
	}
	providers, err := parseCatalogProviders(data)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(opts.CachePath)
	if err != nil {
		return providers, false, nil
	}
	fresh := opts.TTL <= 0 || time.Since(info.ModTime()) <= opts.TTL
	return providers, fresh, nil
}

func fetchCatalog(ctx context.Context, opts catalogOptions) (map[string]catalogProvider, error) {
	if strings.TrimSpace(opts.URL) == "" {
		return nil, errCatalogUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return nil, err
	}

	client := NewHTTPClient(opts.Insecure)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading models catalog response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models catalog returned %d: %s", resp.StatusCode, body)
	}

	providers, err := parseCatalogProviders(body)
	if err != nil {
		return nil, err
	}
	if err := writeCatalogCache(opts.CachePath, body); err != nil {
		return nil, err
	}
	return providers, nil
}

func parseCatalogProviders(data []byte) (map[string]catalogProvider, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("invalid models catalog JSON: %w", err)
	}

	providers := make(map[string]catalogProvider)
	for key, raw := range top {
		switch key {
		case "$schema":
			continue
		case "providers":
			var nested map[string]catalogProvider
			if err := json.Unmarshal(raw, &nested); err != nil {
				return nil, fmt.Errorf("invalid models catalog providers section: %w", err)
			}
			for nestedKey, provider := range nested {
				providers[strings.ToLower(strings.TrimSpace(nestedKey))] = provider
			}
			continue
		}

		var provider catalogProvider
		if err := json.Unmarshal(raw, &provider); err != nil {
			continue
		}
		if provider.Models == nil {
			continue
		}
		providers[strings.ToLower(strings.TrimSpace(key))] = provider
	}

	if len(providers) == 0 {
		return nil, errors.New("models catalog did not contain providers")
	}
	return providers, nil
}

func writeCatalogCache(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating models catalog cache dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("writing models catalog cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("finalizing models catalog cache: %w", err)
	}
	return nil
}

func readCatalogOverrides(path string) (map[string]catalogProvider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var wrapped catalogOverridesFile
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Providers) > 0 {
		providers := make(map[string]catalogProvider, len(wrapped.Providers))
		for key, provider := range wrapped.Providers {
			providers[strings.ToLower(strings.TrimSpace(key))] = provider
		}
		return providers, nil
	}

	return parseCatalogProviders(data)
}

func mergeCatalogProviders(base, overrides map[string]catalogProvider) map[string]catalogProvider {
	merged := make(map[string]catalogProvider, len(base)+len(overrides))
	maps.Copy(merged, base)
	for key, provider := range overrides {
		if existing, ok := merged[key]; ok {
			merged[key] = mergeCatalogProvider(existing, provider)
			continue
		}
		merged[key] = provider
	}
	return merged
}

func mergeCatalogProvider(base, override catalogProvider) catalogProvider {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.API) != "" {
		result.API = override.API
	}
	if result.Models == nil {
		result.Models = make(map[string]catalogModel)
	}
	for key, model := range override.Models {
		existing, exists := result.Models[key]
		if !exists {
			result.Models[key] = model
			continue
		}
		result.Models[key] = mergeCatalogModel(existing, model)
	}
	return result
}

func mergeCatalogModel(base, override catalogModel) catalogModel {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.Name) != "" {
		result.Name = override.Name
	}
	if override.Reasoning {
		result.Reasoning = true
	}
	if override.Context > 0 {
		result.Context = override.Context
	}
	if override.Output > 0 {
		result.Output = override.Output
	}
	if override.Cost.Input != 0 {
		result.Cost.Input = override.Cost.Input
	}
	if override.Cost.Output != 0 {
		result.Cost.Output = override.Cost.Output
	}
	if override.Cost.CacheRead != 0 {
		result.Cost.CacheRead = override.Cost.CacheRead
	}
	if override.Cost.CacheWrite != 0 {
		result.Cost.CacheWrite = override.Cost.CacheWrite
	}
	return result
}
