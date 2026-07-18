package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/providers"
)

// apiKeyValidationTimeout bounds the live credential probe RunSetup performs
// before persisting an API key, so an unreachable or slow provider fails fast
// instead of stalling the setup flow. A var, not a const, so tests can shrink
// it to keep a deliberately-slow-server test fast.
var apiKeyValidationTimeout = 5 * time.Second

// anthropicAPIVersion mirrors the version ai-sdk's native Anthropic client
// sends (provider/anthropic/anthropic.go) so the validation probe is
// accepted by the same API surface the runtime actually uses.
const anthropicAPIVersion = "2023-06-01"

// apiKeyValidationOutcome classifies the result of a live API-key probe.
type apiKeyValidationOutcome int

const (
	// apiKeyValid means the provider accepted the credential.
	apiKeyValid apiKeyValidationOutcome = iota
	// apiKeyRejected means the provider gave a definite "this credential is
	// wrong" answer (401/403) - retrying with the same key will not help.
	apiKeyRejected
	// apiKeyInconclusive covers everything else that stops validation from
	// reaching a definite answer: network errors, timeouts, and unexpected
	// provider responses. The key might still be valid.
	apiKeyInconclusive
)

// apiKeyValidationResult is the outcome of a live API-key probe plus enough
// detail to build an actionable message. err is diagnostic only - it is
// derived from the HTTP transport/status, never from the key itself, so it
// is always safe to display or wrap into an error message.
type apiKeyValidationResult struct {
	outcome apiKeyValidationOutcome
	err     error
}

// validateAPIKey performs a bounded live check that key authenticates
// against entry's provider before RunSetup persists it. It is a package var,
// not a plain function, so tests can stub it out and exercise RunSetup's
// retry/override/proceed flow without making real network calls; the real
// implementation (liveValidateAPIKey) is exercised directly against an
// httptest server instead.
var validateAPIKey = liveValidateAPIKey

// liveValidateAPIKey issues a single bounded GET request that every catalog
// AuthAPIKey provider accepts with just a credential, and classifies the
// response. OpenAI-compatible providers (the default class) are probed at
// GET {baseURL}/models with a Bearer token; Anthropic's native Messages API
// is probed at GET {baseURL}/v1/models with x-api-key plus anthropic-version,
// per internal/providers/catalog.go's Class field. The key is never logged or
// placed anywhere but the request header.
func liveValidateAPIKey(ctx context.Context, entry providers.CatalogEntry, key string, insecure bool) apiKeyValidationResult {
	ctx, cancel := context.WithTimeout(ctx, apiKeyValidationTimeout)
	defer cancel()

	endpoint, headers := apiKeyValidationRequest(entry, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return apiKeyValidationResult{outcome: apiKeyInconclusive, err: err}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: apiKeyValidationTimeout}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in via --insecure
	}

	resp, err := client.Do(req)
	if err != nil {
		return apiKeyValidationResult{outcome: apiKeyInconclusive, err: err}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return apiKeyValidationResult{outcome: apiKeyValid}
	case http.StatusUnauthorized, http.StatusForbidden:
		return apiKeyValidationResult{outcome: apiKeyRejected, err: statusError(resp.StatusCode)}
	default:
		return apiKeyValidationResult{outcome: apiKeyInconclusive, err: statusError(resp.StatusCode)}
	}
}

// apiKeyValidationRequest builds the endpoint and headers for entry's
// validation probe. key is placed only in the returned header map.
func apiKeyValidationRequest(entry providers.CatalogEntry, key string) (endpoint string, headers map[string]string) {
	baseURL := strings.TrimRight(entry.BaseURL, "/")
	if entry.Class == "anthropic" {
		return baseURL + "/v1/models", map[string]string{
			"x-api-key":         key,
			"anthropic-version": anthropicAPIVersion,
		}
	}
	return baseURL + "/models", map[string]string{"Authorization": "Bearer " + key}
}

func statusError(code int) error {
	return fmt.Errorf("status %d", code)
}
