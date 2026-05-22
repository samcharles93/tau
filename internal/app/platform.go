package app

import (
	"context"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/maas"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

// TokenOptions holds the parameters for minting a MaaS token.
type TokenOptions struct {
	Endpoint platform.Endpoint
	Insecure bool
	OCPToken string
	Expiry   string
}

// MintToken resolves a fresh MaaS JWT, optionally using a pre-existing OCP token.
func MintToken(ctx context.Context, opts TokenOptions) (string, error) {
	return maas.ResolveMaaSToken(ctx, opts.Endpoint, opts.Insecure, "", opts.OCPToken, opts.Expiry)
}

// ModelsOptions holds the parameters for discovering available models.
type ModelsOptions struct {
	Endpoint  platform.Endpoint
	Insecure  bool
	MaaSToken string
	OCPToken  string
}

// DiscoverModels resolves a MaaS token and returns the list of available models.
func DiscoverModels(ctx context.Context, opts ModelsOptions) ([]maas.Model, error) {
	maasToken, err := maas.ResolveMaaSToken(ctx, opts.Endpoint, opts.Insecure, opts.MaaSToken, opts.OCPToken, "")
	if err != nil {
		return nil, err
	}

	return maas.DiscoverModels(ctx, opts.Endpoint, maasToken, opts.Insecure)
}
