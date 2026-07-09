package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tauconfig "github.com/samcharles93/tau/internal/config"
)

func TestCodexModelsRequestsCodexModelsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(codexModelsResponse{Models: []codexModelInfo{{Slug: "gpt-5.5"}}})
	}))
	defer srv.Close()

	provider := tauconfig.ProviderConfig{Name: "openai-codex", BaseURL: srv.URL + "/backend-api"}
	models, err := codexModels(context.Background(), provider, false)
	if err != nil {
		t.Fatalf("codexModels() error = %v", err)
	}
	if gotPath != "/backend-api/codex/models" {
		t.Fatalf("requested path = %q, want /backend-api/codex/models", gotPath)
	}
	if len(models) != 1 || models[0].Slug != "gpt-5.5" {
		t.Fatalf("models = %#v, want one gpt-5.5 entry", models)
	}
}

func TestCodexModelRefsFromInfosUsesLiveSlugs(t *testing.T) {
	provider := tauconfig.ProviderConfig{Name: "openai-codex", BaseURL: "https://chatgpt.com/backend-api"}
	refs := codexModelRefsFromInfos(provider, []codexModelInfo{
		{
			Slug:                     "gpt-5.5",
			DisplayName:              "gpt-5.5",
			DefaultReasoningLevel:    "medium",
			SupportedReasoningLevels: []string{"low", "medium", "high", "xhigh"},
			ContextWindow:            272000,
			MaxOutputTokens:          128000,
		},
		{
			Slug:        "gpt-5.4-mini",
			DisplayName: "gpt-5.4-mini",
		},
	})

	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].ID != "gpt-5.5" {
		t.Fatalf("refs[0].ID = %q, want gpt-5.5", refs[0].ID)
	}
	if refs[0].Config.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %q, want medium", refs[0].Config.ReasoningEffort)
	}
	if got := refs[0].Config.ReasoningEfforts; len(got) != 4 || got[3] != "xhigh" {
		t.Fatalf("reasoning efforts = %#v, want low/medium/high/xhigh", got)
	}
	if refs[1].ID != "gpt-5.4-mini" {
		t.Fatalf("refs[1].ID = %q, want gpt-5.4-mini", refs[1].ID)
	}
}
