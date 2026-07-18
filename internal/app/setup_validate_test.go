package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/providers"
)

func TestLiveValidateAPIKeyOpenAICompatibleValidKey(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: srv.URL + "/v1", Auth: providers.AuthAPIKey}
	result := liveValidateAPIKey(context.Background(), entry, "sk-test-key", false)

	require.Equal(t, apiKeyValid, result.outcome)
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "Bearer sk-test-key", gotAuth)
}

func TestLiveValidateAPIKeyOpenAICompatibleUnauthorizedIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: srv.URL, Auth: providers.AuthAPIKey}
	result := liveValidateAPIKey(context.Background(), entry, "sk-bad-key", false)

	require.Equal(t, apiKeyRejected, result.outcome)
	require.Error(t, result.err)
	assert.NotContains(t, result.err.Error(), "sk-bad-key")
}

func TestLiveValidateAPIKeyOpenAICompatibleForbiddenIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: srv.URL, Auth: providers.AuthAPIKey}
	result := liveValidateAPIKey(context.Background(), entry, "sk-bad-key", false)

	require.Equal(t, apiKeyRejected, result.outcome)
}

func TestLiveValidateAPIKeyAnthropicUsesNativeAuthHeaders(t *testing.T) {
	var gotPath, gotKeyHeader, gotVersion, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKeyHeader = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := providers.CatalogEntry{ID: "anthropic", DisplayName: "Anthropic (Claude)", BaseURL: srv.URL, Auth: providers.AuthAPIKey, Class: "anthropic"}
	result := liveValidateAPIKey(context.Background(), entry, "anthro-test-key", false)

	require.Equal(t, apiKeyValid, result.outcome)
	assert.Equal(t, "/v1/models", gotPath)
	assert.Equal(t, "anthro-test-key", gotKeyHeader)
	assert.Equal(t, "2023-06-01", gotVersion)
	assert.Empty(t, gotAuth, "anthropic must not receive a Bearer Authorization header")
}

func TestLiveValidateAPIKeyNetworkErrorIsInconclusive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable := srv.URL
	srv.Close() // closed server: connection refused

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: unreachable, Auth: providers.AuthAPIKey}
	result := liveValidateAPIKey(context.Background(), entry, "sk-test-key", false)

	require.Equal(t, apiKeyInconclusive, result.outcome)
	require.Error(t, result.err)
}

func TestLiveValidateAPIKeyTimesOutBounded(t *testing.T) {
	orig := apiKeyValidationTimeout
	apiKeyValidationTimeout = 50 * time.Millisecond
	t.Cleanup(func() { apiKeyValidationTimeout = orig })

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	// srv.Close() blocks until in-flight handlers return, so close(block)
	// must run first: defer LIFO order means it must be deferred second.
	defer srv.Close()
	defer close(block)

	entry := providers.CatalogEntry{ID: "openai", DisplayName: "OpenAI", BaseURL: srv.URL, Auth: providers.AuthAPIKey}

	start := time.Now()
	result := liveValidateAPIKey(context.Background(), entry, "sk-test-key", false)
	elapsed := time.Since(start)

	require.Equal(t, apiKeyInconclusive, result.outcome)
	assert.Less(t, elapsed, 2*time.Second, "validation must respect the bounded timeout")
}
