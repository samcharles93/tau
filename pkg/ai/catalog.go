// Package ai is Tau's AI SDK integration layer.
//
// It wraps the github.com/samcharles93/ai-sdk library so the rest of tau
// consumes a single, provider-agnostic surface: model discovery from
// models.dev, provider resolution, and streaming chat completion via the
// agent.Streamer interface.
package ai

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
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

const (
	defaultCatalogURL   = "https://models.dev/api.json"
	defaultCatalogTTL   = 24 * time.Hour
	modelsCacheFile     = "models.json"
	modelsOverridesFile = "api.overrides.json"
	modelsCatalogURLEnv = "TAU_MODELS_CATALOG_URL"
	modelsCatalogTTLEnv = "TAU_MODELS_CATALOG_TTL"
)

// ErrCatalogUnavailable is returned when the models.dev catalog cannot be
// loaded from cache or network.
var ErrCatalogUnavailable = errors.New("ai: model catalog unavailable")

// CatalogOptions configures where the catalog is loaded from and how fresh it
// must be.
type CatalogOptions struct {
	URL           string
	CachePath     string
	OverridesPath string
	TTL           time.Duration
	Insecure      bool
}

// DefaultCatalogOptions returns options that read from the standard tau config
// directory, respecting TAU_MODELS_CATALOG_URL and TAU_MODELS_CATALOG_TTL.
func DefaultCatalogOptions(insecure bool) CatalogOptions {
	ttl := defaultCatalogTTL
	if raw := strings.TrimSpace(os.Getenv(modelsCatalogTTLEnv)); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			ttl = parsed
		}
	}
	url := strings.TrimSpace(os.Getenv(modelsCatalogURLEnv))
	if url == "" {
		url = defaultCatalogURL
	}
	baseDir := config.Dir()
	return CatalogOptions{
		URL:           url,
		CachePath:     filepath.Join(baseDir, modelsCacheFile),
		OverridesPath: filepath.Join(baseDir, modelsOverridesFile),
		TTL:           ttl,
		Insecure:      insecure,
	}
}

// Catalog is the in-memory view of the models.dev provider/model metadata,
// optionally merged with a local overrides file.
type Catalog struct {
	opts      CatalogOptions
	mu        sync.RWMutex
	providers map[string]CatalogProvider
	fetchedAt time.Time
}

// NewCatalog creates an empty catalog. Call Load or Fetch before use.
func NewCatalog(opts CatalogOptions) *Catalog {
	return &Catalog{opts: opts}
}

// CatalogProvider mirrors models.dev's provider entry.
type CatalogProvider struct {
	ID     string                  `json:"id"`
	NPM    string                  `json:"npm,omitempty"`
	API    string                  `json:"api,omitempty"`
	Env    []string                `json:"env,omitempty"`
	Models map[string]CatalogModel `json:"models,omitempty"`
}

// CatalogModel mirrors models.dev's model entry.
type CatalogModel struct {
	ID          string      `json:"id"`
	Name        string      `json:"name,omitempty"`
	Context     int         `json:"context,omitempty"`
	Output      int         `json:"output,omitempty"`
	Reasoning   bool        `json:"reasoning,omitempty"`
	Attachment  bool        `json:"attachment,omitempty"`
	ToolCall    bool        `json:"tool_call,omitempty"`
	Temperature bool        `json:"temperature,omitempty"`
	Cost        CatalogCost `json:"cost"`
}

// CatalogCost mirrors models.dev's cost entry.
type CatalogCost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// Provider returns a single provider by name, or false if it is not present.
func (c *Catalog) Provider(name string) (CatalogProvider, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.providers[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// Models returns the models for a provider in deterministic order, or an
// error if the provider is not in the catalog.
func (c *Catalog) Models(name string) ([]CatalogModel, error) {
	p, ok := c.Provider(name)
	if !ok {
		return nil, fmt.Errorf("%w: provider %q not found", ErrCatalogUnavailable, name)
	}
	ids := make([]string, 0, len(p.Models))
	for id := range p.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	models := make([]CatalogModel, 0, len(ids))
	for _, id := range ids {
		m := p.Models[id]
		if strings.TrimSpace(m.ID) == "" {
			m.ID = id
		}
		models = append(models, m)
	}
	return models, nil
}

// Model looks up a specific model under a provider.
func (c *Catalog) Model(providerName, modelID string) (CatalogModel, bool) {
	p, ok := c.Provider(providerName)
	if !ok {
		return CatalogModel{}, false
	}
	m, ok := p.Models[strings.TrimSpace(modelID)]
	if !ok {
		return CatalogModel{}, false
	}
	if strings.TrimSpace(m.ID) == "" {
		m.ID = modelID
	}
	return m, true
}

// APIKeyEnv returns the first environment variable name advertised by the
// catalog for a provider, or false if none is declared.
func (c *Catalog) APIKeyEnv(providerName string) (string, bool) {
	p, ok := c.Provider(providerName)
	if !ok {
		return "", false
	}
	for _, env := range p.Env {
		if strings.TrimSpace(env) != "" {
			return env, true
		}
	}
	return "", false
}

// API returns the provider's default API base URL from the catalog, or false.
func (c *Catalog) API(providerName string) (string, bool) {
	p, ok := c.Provider(providerName)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(p.API) == "" {
		return "", false
	}
	return strings.TrimRight(p.API, "/"), true
}

// NPM returns the provider's ai-sdk npm package identifier, or false.
func (c *Catalog) NPM(providerName string) (string, bool) {
	p, ok := c.Provider(providerName)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(p.NPM) == "" {
		return "", false
	}
	return p.NPM, true
}

// Load populates the catalog from disk cache if it is fresh, otherwise fetches
// it from the configured URL.
func (c *Catalog) Load(ctx context.Context) error {
	providers, fresh, err := c.readCache()
	if err == nil && fresh {
		overrides, _ := c.readOverrides()
		c.setProviders(mergeCatalogProviders(providers, overrides), time.Now())
		return nil
	}

	if err := c.Fetch(ctx); err != nil {
		// Stale cache is better than nothing.
		if providers != nil {
			overrides, _ := c.readOverrides()
			c.setProviders(mergeCatalogProviders(providers, overrides), time.Now().Add(-c.opts.TTL-1))
			return nil
		}
		return err
	}
	return nil
}

// Fetch forces a network refresh of the catalog and writes it to disk.
func (c *Catalog) Fetch(ctx context.Context) error {
	body, err := c.fetchCatalog(ctx)
	if err != nil {
		return err
	}
	providers, err := parseCatalogProviders(body)
	if err != nil {
		return err
	}
	if err := c.writeCache(body); err != nil {
		return err
	}
	overrides, _ := c.readOverrides()
	c.setProviders(mergeCatalogProviders(providers, overrides), time.Now())
	return nil
}

func (c *Catalog) setProviders(providers map[string]CatalogProvider, fetchedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = providers
	c.fetchedAt = fetchedAt
}

func (c *Catalog) readCache() (map[string]CatalogProvider, bool, error) {
	data, err := os.ReadFile(c.opts.CachePath)
	if err != nil {
		return nil, false, err
	}
	providers, err := parseCatalogProviders(data)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(c.opts.CachePath)
	if err != nil {
		return providers, false, nil
	}
	fresh := c.opts.TTL <= 0 || time.Since(info.ModTime()) <= c.opts.TTL
	return providers, fresh, nil
}

func (c *Catalog) writeCache(body []byte) error {
	dir := filepath.Dir(c.opts.CachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating catalog cache dir: %w", err)
	}
	tmp := c.opts.CachePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("writing catalog cache: %w", err)
	}
	if err := os.Rename(tmp, c.opts.CachePath); err != nil {
		return fmt.Errorf("finalizing catalog cache: %w", err)
	}
	return nil
}

func (c *Catalog) readOverrides() (map[string]CatalogProvider, error) {
	data, err := os.ReadFile(c.opts.OverridesPath)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Providers map[string]CatalogProvider `json:"providers"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Providers) > 0 {
		return normalizeProviderKeys(wrapped.Providers), nil
	}
	return parseCatalogProviders(data)
}

func (c *Catalog) fetchCatalog(ctx context.Context) ([]byte, error) {
	if strings.TrimSpace(c.opts.URL) == "" {
		return nil, fmt.Errorf("%w: no catalog URL configured", ErrCatalogUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.URL, nil)
	if err != nil {
		return nil, err
	}
	client := newHTTPClient(c.opts.Insecure, 30*time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch catalog: %v", ErrCatalogUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: reading catalog response: %v", ErrCatalogUnavailable, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: catalog returned %d: %s", ErrCatalogUnavailable, resp.StatusCode, body)
	}
	return body, nil
}

func parseCatalogProviders(data []byte) (map[string]CatalogProvider, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%w: invalid catalog JSON: %v", ErrCatalogUnavailable, err)
	}
	providers := make(map[string]CatalogProvider)
	for key, raw := range top {
		if key == "$schema" {
			continue
		}
		if key == "providers" {
			var nested map[string]CatalogProvider
			if err := json.Unmarshal(raw, &nested); err != nil {
				return nil, fmt.Errorf("%w: invalid providers section: %v", ErrCatalogUnavailable, err)
			}
			for nestedKey, p := range nested {
				providers[normalizeProviderKey(nestedKey)] = p
			}
			continue
		}
		var p CatalogProvider
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if len(p.Models) == 0 && strings.TrimSpace(p.API) == "" && strings.TrimSpace(p.NPM) == "" {
			continue
		}
		providers[normalizeProviderKey(key)] = p
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: catalog did not contain any providers", ErrCatalogUnavailable)
	}
	return providers, nil
}

func normalizeProviderKeys(providers map[string]CatalogProvider) map[string]CatalogProvider {
	out := make(map[string]CatalogProvider, len(providers))
	for key, p := range providers {
		out[normalizeProviderKey(key)] = p
	}
	return out
}

func normalizeProviderKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func mergeCatalogProviders(base, overrides map[string]CatalogProvider) map[string]CatalogProvider {
	merged := make(map[string]CatalogProvider, len(base)+len(overrides))
	maps.Copy(merged, base)
	for key, override := range overrides {
		existing, exists := merged[key]
		if !exists {
			merged[key] = override
			continue
		}
		merged[key] = mergeCatalogProvider(existing, override)
	}
	return merged
}

func mergeCatalogProvider(base, override CatalogProvider) CatalogProvider {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.NPM) != "" {
		result.NPM = override.NPM
	}
	if strings.TrimSpace(override.API) != "" {
		result.API = override.API
	}
	if len(override.Env) > 0 {
		result.Env = override.Env
	}
	if result.Models == nil {
		result.Models = make(map[string]CatalogModel)
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

func mergeCatalogModel(base, override CatalogModel) CatalogModel {
	result := base
	if strings.TrimSpace(override.ID) != "" {
		result.ID = override.ID
	}
	if strings.TrimSpace(override.Name) != "" {
		result.Name = override.Name
	}
	if override.Context > 0 {
		result.Context = override.Context
	}
	if override.Output > 0 {
		result.Output = override.Output
	}
	if override.Reasoning {
		result.Reasoning = true
	}
	if override.Attachment {
		result.Attachment = true
	}
	if override.ToolCall {
		result.ToolCall = true
	}
	if override.Temperature {
		result.Temperature = true
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
