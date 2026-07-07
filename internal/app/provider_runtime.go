package app

import (
	"context"
	"sync"

	"github.com/samcharles93/ai-sdk/pkg/runtime"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

// providerRuntime holds the ai-sdk runtime together with the set of usable
// providers it was built from. It can be rebuilt live when provider state
// changes (e.g. after /provider), so both the dynamic streamer and the
// model refresher always observe the current provider set without a restart.
type providerRuntime struct {
	insecure bool

	mu        sync.RWMutex
	rt        *runtime.Runtime
	providers []tauconfig.ProviderConfig
}

// newProviderRuntime wraps an already-built runtime and its provider set.
func newProviderRuntime(rt *runtime.Runtime, provs []tauconfig.ProviderConfig, insecure bool) *providerRuntime {
	return &providerRuntime{rt: rt, providers: provs, insecure: insecure}
}

// runtime returns the current runtime for per-turn provider resolution.
func (p *providerRuntime) runtime() *runtime.Runtime {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rt
}

// snapshot returns the current runtime and provider set together, taken under
// a single lock so they are consistent with each other.
func (p *providerRuntime) snapshot() (*runtime.Runtime, []tauconfig.ProviderConfig) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.rt, p.providers
}

// reload re-resolves the usable providers from current state and environment,
// rebuilds the runtime around them, and force-refreshes the model catalogue.
// On success the new runtime and provider set are swapped in atomically; on
// failure the existing runtime is left untouched. Safe for concurrent use.
func (p *providerRuntime) reload(_ context.Context) error {
	cfg, _, err := providers.Effective()
	if err != nil {
		return err
	}
	// newRuntimeForProviders loads the embedded model snapshot, so the rebuilt
	// runtime sees every provider just enabled without any network access.
	rt := newRuntimeForProviders(cfg.Providers, p.insecure)

	p.mu.Lock()
	p.rt = rt
	p.providers = cfg.Providers
	p.mu.Unlock()
	return nil
}
