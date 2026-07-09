package app

import (
	"context"

	"github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/config"
)

// TokenOptions holds the parameters for resolving a provider bearer token.
type TokenOptions struct {
	Provider config.ProviderConfig
	Insecure bool
}

// ResolveToken resolves the bearer token for the configured provider
// using the ai-sdk runtime's built-in auth resolvers.
func ResolveToken(ctx context.Context, opts TokenOptions) (string, error) {
	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)
	auth, err := rt.ResolveAuth(ctx, opts.Provider.Name)
	if err != nil {
		return "", err
	}
	return auth.Token, nil
}

type ModelsOptions struct {
	Provider config.ProviderConfig
	Insecure bool
}

// DiscoverModels resolves a provider token and returns the list of
// available models via the ai-sdk runtime.
func DiscoverModels(ctx context.Context, opts ModelsOptions) ([]runtime.ModelInfo, error) {
	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)
	if err := rt.LoadCatalog(ctx); err != nil {
		return nil, err
	}
	return rt.Models(opts.Provider.Name)
}

// RefreshModels forces a refresh of the models.dev catalog and returns
// the refreshed list of models for the configured provider.
func RefreshModels(ctx context.Context, opts ModelsOptions) ([]runtime.ModelInfo, error) {
	rt := newRuntimeForProvider(opts.Provider, opts.Insecure)
	if err := rt.Catalog().Fetch(ctx); err != nil {
		return nil, err
	}
	return rt.Models(opts.Provider.Name)
}
