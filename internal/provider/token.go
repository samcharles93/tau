package provider

import (
	"context"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/platform"
)

// ResolveBearerToken returns a bearer token for a configured provider.
func ResolveBearerToken(ctx context.Context, provider config.ProviderConfig, insecure bool) (string, error) {
	return platform.ResolveBearerToken(ctx, provider, insecure)
}
