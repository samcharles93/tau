package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

// liveModelTimeout bounds the per-provider /models probe so an unreachable
// endpoint (e.g. a stopped local Ollama server) fails fast instead of stalling
// model discovery.
const liveModelTimeout = 4 * time.Second

// liveModelRefs lists a provider's models at runtime from its OpenAI-compatible
// /models endpoint, for providers whose model set is dynamic and not baked into
// the embedded snapshot (e.g. a local Ollama server). Models are returned in the
// order the endpoint reports them, tagged with the provider. The generic
// /models endpoint carries no capability data, so no tool-call filtering is
// applied here; for the "ollama" provider specifically, capabilities (e.g.
// "thinking") are additionally probed via Ollama's native /api/tags so
// reasoning-capable models get selectable effort levels (see
// ollamaThinkingModels below) instead of silently having none.
func liveModelRefs(ctx context.Context, provider tauconfig.ProviderConfig, insecure bool) ([]tauchat.ChatModelRef, error) {
	ids, err := liveModelIDs(ctx, provider.BaseURL, providerAPIKey(provider), provider.Headers, insecure)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(provider.BaseURL, "/")

	var thinking map[string]bool
	if provider.Name == "ollama" {
		// Best-effort enrichment: a probe failure (older Ollama, custom
		// proxy without /api/tags, ...) must not fail model listing.
		thinking, err = ollamaThinkingModels(ctx, baseURL, providerAPIKey(provider), insecure)
		if err != nil {
			slog.Debug("ollama capability probe failed", "err", err)
		}
	}

	refs := make([]tauchat.ChatModelRef, 0, len(ids))
	for _, id := range ids {
		cfg := tauconfig.ModelConfig{ID: id}
		if thinking[id] {
			// No output-token budget is available from either endpoint
			// here, so fall back to the same sensible floor used for
			// snapshot-sourced reasoning models with no advertised
			// effort options (see modelInfoToModelConfig).
			cfg.Reasoning = true
			cfg.ReasoningEfforts = []string{"low", "medium", "high"}
			cfg.ReasoningBudgetMax = 8192
		}
		refs = append(refs, tauchat.ChatModelRef{
			ID:       id,
			URL:      baseURL,
			Provider: provider.Name,
			Config:   cfg,
		})
	}
	return refs, nil
}

// ollamaThinkingModels queries Ollama's native /api/tags (a sibling of the
// OpenAI-compatible /v1 surface at the same host) and returns the set of
// model names that advertise the "thinking" capability. baseURL is the
// OpenAI-compatible base (e.g. "http://localhost:11434/v1"); the /v1 suffix
// is stripped to reach the native API root.
func ollamaThinkingModels(ctx context.Context, baseURL, apiKey string, insecure bool) (map[string]bool, error) {
	root := strings.TrimSuffix(baseURL, "/v1")
	endpoint := root + "/api/tags"
	ctx, cancel := context.WithTimeout(ctx, liveModelTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: liveModelTimeout}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in via --insecure
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tags: status %d", resp.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name         string   `json:"name"`
			Capabilities []string `json:"capabilities"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}

	out := make(map[string]bool, len(payload.Models))
	for _, m := range payload.Models {
		if slices.Contains(m.Capabilities, "thinking") {
			out[m.Name] = true
		}
	}
	return out, nil
}

// liveModelIDs performs the GET {baseURL}/models call and returns the model IDs.
func liveModelIDs(ctx context.Context, baseURL, apiKey string, headers map[string]string, insecure bool) ([]string, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	ctx, cancel := context.WithTimeout(ctx, liveModelTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}

	client := &http.Client{Timeout: liveModelTimeout}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in via --insecure
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models: status %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if strings.TrimSpace(m.ID) != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// providerAPIKey resolves the bearer token for a provider config from its
// literal key or the named environment variable. Returns "" when the provider
// needs no credential (e.g. a local Ollama server).
func providerAPIKey(p tauconfig.ProviderConfig) string {
	if k := strings.TrimSpace(p.Auth.APIKey); k != "" {
		return k
	}
	if env := strings.TrimSpace(p.Auth.APIKeyEnv); env != "" {
		return os.Getenv(env)
	}
	return ""
}
