package app

import (
	"context"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/provider"
)

// TokenOptions holds the parameters for resolving a provider bearer token.
type TokenOptions struct {
	Provider config.ProviderConfig
	Insecure bool
}

// ResolveToken resolves the bearer token for the configured provider.
func ResolveToken(ctx context.Context, opts TokenOptions) (string, error) {
	return provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
}

// ModelsOptions holds the parameters for discovering available models.
type ModelsOptions struct {
	Provider config.ProviderConfig
	Insecure bool
}

// DiscoverModels resolves a provider token and returns the list of available models.
func DiscoverModels(ctx context.Context, opts ModelsOptions) ([]provider.Model, error) {
	bearerToken, err := provider.ResolveBearerToken(ctx, opts.Provider, opts.Insecure)
	if err != nil {
		return nil, err
	}
	return provider.DiscoverModels(ctx, opts.Provider, bearerToken, opts.Insecure)
}

// RefreshModels forces a refresh of the models.dev catalog and returns the
// refreshed list of models for the configured provider.
func RefreshModels(ctx context.Context, opts ModelsOptions) ([]provider.Model, error) {
	return provider.RefreshModels(ctx, opts.Provider, opts.Insecure)
}
