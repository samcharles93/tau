package app

import (
	"os"
	"path/filepath"
	"testing"
)

// A minimal models.dev-format catalog using models.dev provider keys ("google",
// "openai") that differ from tau's IDs ("gemini", "openai-codex").
const fakePersistedCatalog = `{
  "google": {
    "id": "google",
    "models": {
      "gemini-fresh-9": {"id": "gemini-fresh-9", "name": "Gemini Fresh 9", "tool_call": true, "limit": {"context": 1000000, "output": 65536}}
    }
  },
  "openai": {
    "id": "openai",
    "models": {
      "gpt-fresh-9": {"id": "gpt-fresh-9", "name": "GPT Fresh 9", "tool_call": true, "limit": {"context": 400000, "output": 128000}}
    }
  }
}`

func TestLoadModelCatalog_NoCacheUsesEmbedded(t *testing.T) {
	cat, err := loadModelCatalog(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cat.Provider("anthropic"); !ok {
		t.Fatal("expected the embedded snapshot to provide anthropic")
	}
}

func TestLoadModelCatalog_EmptyCachePath(t *testing.T) {
	cat, err := loadModelCatalog("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cat.Provider("anthropic"); !ok {
		t.Fatal("expected the embedded snapshot to provide anthropic")
	}
}

// A persisted catalog (written by `tau refresh`) must be merged over the
// embedded snapshot and re-keyed from models.dev provider IDs onto tau IDs, so
// freshly published models appear under the names tau queries with.
func TestLoadModelCatalog_PersistedMergedAndRekeyed(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "models-catalog.json")
	if err := os.WriteFile(cache, []byte(fakePersistedCatalog), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := loadModelCatalog(cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Embedded snapshot still present.
	if _, ok := cat.Provider("anthropic"); !ok {
		t.Fatal("embedded snapshot entries must survive the merge")
	}

	// models.dev "google" re-keyed to tau "gemini"; the fresh model is visible.
	geminiModels, err := cat.Models("gemini")
	if err != nil {
		t.Fatalf("gemini lookup failed after re-key: %v", err)
	}
	found := false
	for _, m := range geminiModels {
		if m.ID == "gemini-fresh-9" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected merged catalog to contain gemini-fresh-9 under tau ID gemini, got %v", geminiModels)
	}

	// models.dev "openai" re-keyed to tau "openai-codex".
	if _, err := cat.Models("openai-codex"); err != nil {
		t.Fatalf("openai-codex lookup failed after re-key: %v", err)
	}
}

// A corrupt cache file must not break startup - the embedded snapshot is used.
func TestLoadModelCatalog_CorruptCacheFallsBack(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "models-catalog.json")
	if err := os.WriteFile(cache, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := loadModelCatalog(cache)
	if err != nil {
		t.Fatalf("corrupt cache must not fail startup: %v", err)
	}
	if _, ok := cat.Provider("anthropic"); !ok {
		t.Fatal("expected embedded snapshot fallback for corrupt cache")
	}
}
