package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
// order the endpoint reports them, tagged with the provider. Capability data is
// unavailable from /models, so no tool-call filtering is applied here.
func liveModelRefs(ctx context.Context, provider tauconfig.ProviderConfig, insecure bool) ([]tauchat.ChatModelRef, error) {
	ids, err := liveModelIDs(ctx, provider.BaseURL, providerAPIKey(provider), insecure)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	refs := make([]tauchat.ChatModelRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, tauchat.ChatModelRef{
			ID:       id,
			URL:      baseURL,
			Provider: provider.Name,
			Config:   tauconfig.ModelConfig{ID: id},
		})
	}
	return refs, nil
}

// liveModelIDs performs the GET {baseURL}/models call and returns the model IDs.
func liveModelIDs(ctx context.Context, baseURL, apiKey string, insecure bool) ([]string, error) {
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
