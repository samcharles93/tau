package ai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	aisdkchat "github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/provider/anthropic"
	"github.com/samcharles93/ai-sdk/pkg/provider/azure"
	"github.com/samcharles93/ai-sdk/pkg/provider/cohere"
	"github.com/samcharles93/ai-sdk/pkg/provider/deepseek"
	"github.com/samcharles93/ai-sdk/pkg/provider/gemini"
	"github.com/samcharles93/ai-sdk/pkg/provider/groq"
	"github.com/samcharles93/ai-sdk/pkg/provider/mistral"
	"github.com/samcharles93/ai-sdk/pkg/provider/ollama"
	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/provider/perplexity"
	"github.com/samcharles93/ai-sdk/pkg/provider/xai"
)

// ChatProviderConfig is the minimal information needed to construct an
// ai-sdk chat.Provider for a tau provider/model.
type ChatProviderConfig struct {
	ProviderID string
	ModelID    string
	NPM        string
	APIKey     string
	BaseURL    string
	Insecure   bool
	Timeout    time.Duration
}

// ResolveChatProvider builds an ai-sdk chat.Provider for the configured
// provider/model using the npm package mapping from the models.dev catalog.
// If the provider cannot be mapped, it returns an error so the caller can fall
// back to tau's legacy OpenAI-compatible streamer.
func ResolveChatProvider(cfg ChatProviderConfig) (aisdkchat.Provider, error) {
	npm := strings.TrimSpace(cfg.NPM)
	if npm == "" {
		return nil, fmt.Errorf("ai: no npm package declared for provider %q", cfg.ProviderID)
	}
	factory, ok := chatFactories[npm]
	if !ok {
		return nil, fmt.Errorf("ai: unsupported ai-sdk package %q for provider %q", npm, cfg.ProviderID)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	httpClient := newHTTPClient(cfg.Insecure, cfg.Timeout)
	return factory(cfg, httpClient)
}

// ResolveAPIKey reads the API key for a provider from the configured
// environment variable, falling back to the catalog's declared env key.
func ResolveAPIKey(providerName, apiKeyEnv string, catalog *Catalog) (string, error) {
	env := strings.TrimSpace(apiKeyEnv)
	if env == "" && catalog != nil {
		if catalogEnv, ok := catalog.APIKeyEnv(providerName); ok {
			env = catalogEnv
		}
	}
	if env != "" {
		if key := strings.TrimSpace(os.Getenv(env)); key != "" {
			return key, nil
		}
	}
	return "", fmt.Errorf("ai: api key not found for provider %q (set %s)", providerName, env)
}

type chatProviderFactory func(cfg ChatProviderConfig, httpClient *http.Client) (aisdkchat.Provider, error)

var chatFactories = map[string]chatProviderFactory{
	"@ai-sdk/deepseek": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return deepseek.New(deepseek.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/openai": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return openai.New(openai.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/anthropic": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return anthropic.New(anthropic.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/azure": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return azure.New(azure.Config{APIKey: cfg.APIKey, Endpoint: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/cohere": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return cohere.New(cohere.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/gemini": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return gemini.New(gemini.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/groq": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return groq.New(groq.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/mistral": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return mistral.New(mistral.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/ollama": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return ollama.New(ollama.Config{BaseURL: cfg.BaseURL, HTTPClient: hc}), nil
	},
	"@ai-sdk/perplexity": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return perplexity.New(perplexity.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
	"@ai-sdk/xai": func(cfg ChatProviderConfig, hc *http.Client) (aisdkchat.Provider, error) {
		return xai.New(xai.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: hc})
	},
}

// WithExtraHeaders attaches provider-specific extra headers to a context so
// that the ai-sdk HTTP client can inject them into outbound requests.
func WithExtraHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return context.WithValue(ctx, extraHeadersKey{}, headers)
}

type extraHeadersKey struct{}

func extraHeadersFrom(ctx context.Context) (map[string]string, bool) {
	h, ok := ctx.Value(extraHeadersKey{}).(map[string]string)
	return h, ok
}
