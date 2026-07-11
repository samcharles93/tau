package providers

import (
	"context"

	"github.com/samcharles93/tau/internal/config"
)

// Effective loads the user's configuration and managed provider state, then
// resolves them (together with the live environment) into a config.Config whose
// Providers are the set tau can actually use right now. The resolved slice is
// returned alongside for richer display (sources, availability) by callers such
// as the /providers command.
//
// Unlike config.LoadConfig, a missing config file and zero hand-written
// providers are not errors: env-detected and OAuth providers stand in.
func Effective(ctx context.Context) (config.Config, []ResolvedProvider, error) {
	cfg, err := config.LoadConfigAllowEmpty()
	if err != nil {
		return config.Config{}, nil, err
	}
	state, err := LoadState()
	if err != nil {
		return config.Config{}, nil, err
	}
	resolved, _ := ResolveWithRefresh(ctx, cfg, state, nil)

	merged := cfg
	merged.Providers = usableProviders(resolved)
	if merged.DefaultProvider == "" && len(merged.Providers) > 0 {
		merged.DefaultProvider = merged.Providers[0].Name
	}
	return merged, resolved, nil
}

// usableProviders extracts the provider configs that have a usable credential
// right now, preserving resolution order.
func usableProviders(resolved []ResolvedProvider) []config.ProviderConfig {
	out := make([]config.ProviderConfig, 0, len(resolved))
	for _, r := range resolved {
		if r.Available {
			out = append(out, r.Config)
		}
	}
	return out
}
