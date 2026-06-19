package ai

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogParseAndMerge(t *testing.T) {
	base := map[string]CatalogProvider{
		"deepseek": {
			ID:  "deepseek",
			NPM: "@ai-sdk/deepseek",
			API: "https://api.deepseek.com",
			Env: []string{"DEEPSEEK_API_KEY"},
			Models: map[string]CatalogModel{
				"deepseek-v4-pro": {
					ID:      "deepseek-v4-pro",
					Name:    "DeepSeek V4 Pro",
					Context: 128000,
					Output:  8192,
				},
			},
		},
	}
	override := map[string]CatalogProvider{
		"deepseek": {
			Models: map[string]CatalogModel{
				"deepseek-v4-flash": {
					ID:      "deepseek-v4-flash",
					Context: 64000,
					Output:  4096,
				},
			},
		},
	}

	merged := mergeCatalogProviders(base, override)
	p, ok := merged["deepseek"]
	if !ok {
		t.Fatal("expected deepseek provider in merged catalog")
	}
	if p.NPM != "@ai-sdk/deepseek" {
		t.Fatalf("npm = %q, want @ai-sdk/deepseek", p.NPM)
	}
	if len(p.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(p.Models))
	}
}

func TestCatalogLoadFromCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, modelsCacheFile)
	body := []byte(`{
		"deepseek": {
			"id": "deepseek",
			"npm": "@ai-sdk/deepseek",
			"api": "https://api.deepseek.com",
			"env": ["DEEPSEEK_API_KEY"],
			"models": {
				"deepseek-chat": {"id": "deepseek-chat", "context": 64000, "output": 4096}
			}
		}
	}`)
	if err := os.WriteFile(cachePath, body, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cat := NewCatalog(CatalogOptions{
		CachePath:     cachePath,
		OverridesPath: filepath.Join(dir, "missing.json"),
		TTL:           time.Hour,
	})
	if err := cat.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	m, ok := cat.Model("deepseek", "deepseek-chat")
	if !ok {
		t.Fatal("expected deepseek-chat model")
	}
	if m.Context != 64000 {
		t.Fatalf("context = %d, want 64000", m.Context)
	}
	if env, ok := cat.APIKeyEnv("deepseek"); !ok || env != "DEEPSEEK_API_KEY" {
		t.Fatalf("api key env = %q, want DEEPSEEK_API_KEY", env)
	}
}

func TestCatalogProviderNotFound(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, modelsCacheFile)
	_ = os.WriteFile(cachePath, []byte(`{"deepseek": {"id": "deepseek", "npm":"@ai-sdk/deepseek", "api":"https://api.deepseek.com", "models": {}}}`), 0o644)

	cat := NewCatalog(CatalogOptions{CachePath: cachePath, TTL: time.Hour})
	if err := cat.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	_, ok := cat.Provider("unknown")
	if ok {
		t.Fatal("expected unknown provider to be missing")
	}
	_, err := cat.Models("unknown")
	if err == nil {
		t.Fatal("expected error for unknown provider models")
	}
}
