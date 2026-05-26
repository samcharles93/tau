package platform

import (
	"context"

	"github.com/samcharles93/tau/internal/config"
)

// TokenSource resolves a bearer token for the given provider.
type TokenSource func(ctx context.Context, provider config.ProviderConfig) (string, error)
