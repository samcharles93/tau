package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// Regression test (tau-bk7): providerCfg must thread the caller's ctx into
// provider resolution instead of substituting context.Background(), so
// provider config I/O (e.g. the OAuth token refresh inside Effective)
// honours TUI shutdown cancellation. The providerEffective seam captures the
// ctx that actually reaches resolution.
func TestProviderCfgThreadsCallerCtx(t *testing.T) {
	orig := providerEffective
	defer func() { providerEffective = orig }()

	captured := make(chan context.Context, 1)
	providerEffective = func(ctx context.Context) config.Config {
		captured <- ctx
		return config.Config{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a dead ctx must reach Effective, not be replaced by Background

	_ = providerCfg(ctx)

	select {
	case got := <-captured:
		if got != ctx {
			t.Fatalf("providerCfg resolved providers with ctx %v, want the caller's ctx", got)
		}
		if !errors.Is(got.Err(), context.Canceled) {
			t.Fatalf("expected the cancelled ctx to reach provider resolution, got err=%v", got.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("providerCfg did not call through to provider resolution")
	}
}

// Same threading contract for a live (non-cancelled) ctx: it must arrive
// unchanged rather than being swapped for Background.
func TestProviderCfgPassesLiveCtxThrough(t *testing.T) {
	orig := providerEffective
	defer func() { providerEffective = orig }()

	captured := make(chan context.Context, 1)
	providerEffective = func(ctx context.Context) config.Config {
		captured <- ctx
		return config.Config{}
	}

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "sentinel")

	_ = providerCfg(ctx)

	select {
	case got := <-captured:
		if got != ctx {
			t.Fatalf("providerCfg resolved providers with ctx %v, want the caller's ctx", got)
		}
		if got.Value(ctxKey{}) != "sentinel" {
			t.Fatal("expected ctx values to survive the trip through providerCfg")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("providerCfg did not call through to provider resolution")
	}
}
