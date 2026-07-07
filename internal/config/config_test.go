package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigUnmarshalYAMLPopulatesMetrics guards against a real bug:
// Config.UnmarshalYAML's internal rawConfig struct omitted the Metrics
// field entirely, so metrics.dir/session/tui in a user's config.yaml were
// silently discarded on every load — cfg.Metrics always ended up as the Go
// zero value in memory regardless of what was actually written to disk.
func TestConfigUnmarshalYAMLPopulatesMetrics(t *testing.T) {
	var cfg Config
	err := yaml.Unmarshal([]byte(`
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
metrics:
  dir: /custom/metrics/path
  session: true
  tui: true
`), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.Metrics.Dir != "/custom/metrics/path" {
		t.Fatalf("Metrics.Dir = %q, want %q", cfg.Metrics.Dir, "/custom/metrics/path")
	}
	if !cfg.Metrics.Session {
		t.Fatal("Metrics.Session = false, want true")
	}
	if !cfg.Metrics.TUI {
		t.Fatal("Metrics.TUI = false, want true")
	}
}

func TestLoadConfigMergesGlobalAndLocal(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: global
default_model: global-model
providers:
  - name: global
    base_url: https://global.example
    auth:
      type: api_key
      api_key_env: GLOBAL_TOKEN
  - name: shared
    base_url: https://global-shared.example
    auth:
      type: none
`)
	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `default_provider: local
providers:
  - name: shared
    base_url: https://local-shared.example
    auth:
      type: api_key
      api_key: local-token
  - name: local
    base_url: https://local.example
    auth:
      type: none
`)

	cfg, err := LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	if cfg.DefaultProvider != "local" {
		t.Fatalf("DefaultProvider = %q, want local", cfg.DefaultProvider)
	}
	if cfg.DefaultModel != "global-model" {
		t.Fatalf("DefaultModel = %q, want global-model", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(cfg.Providers))
	}
	shared, err := ResolveProvider(cfg, "shared")
	if err != nil {
		t.Fatalf("ResolveProvider(shared) error = %v", err)
	}
	if shared.BaseURL != "https://local-shared.example" || shared.Auth.APIKey != "local-token" {
		t.Fatalf("shared provider did not use local override: %#v", shared)
	}
}

func TestLoadConfigUIShowReasoningDefaultAndLocalOverride(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: local
ui:
  show_reasoning: true
providers:
  - name: local
    base_url: http://localhost:11434
    auth:
      type: none
`)
	cfg, err := LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	if !cfg.UI.ShowReasoning {
		t.Fatal("UI.ShowReasoning = false, want global true")
	}

	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `ui:
  show_reasoning: false
`)
	cfg, err = LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() with local override error = %v", err)
	}
	if cfg.UI.ShowReasoning {
		t.Fatal("UI.ShowReasoning = true, want local override false")
	}

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: local
providers:
  - name: local
    base_url: http://localhost:11434
    auth:
      type: none
`)
	if err := os.Remove(filepath.Join(projectDir, ".tau.yaml")); err != nil {
		t.Fatalf("remove local config: %v", err)
	}
	cfg, err = LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() default error = %v", err)
	}
	if cfg.UI.ShowReasoning {
		t.Fatal("UI.ShowReasoning default = true, want false")
	}
}

func TestLoadConfigAcceptsProviderMapAndAliases(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: deepseek
default_model: deepseek-v4-pro
providers:
  deepseek:
    baseUrl: https://api.deepseek.com
    api: openai-completions
    apiKey: $DEEPSEEK_API_KEY
    models:
      deepseek-v4-pro:
        name: DeepSeek V4 Pro
        contextWindow: 128000
        defaultMaxTokens: 8192
        maxTokens: 32768
        input: [text]
        canReason: true
        thinking:
          min_level: low
          max_level: high
          mode: effort
        cost:
          input: 0.1
          output: 0.2
          cache_read: 0.01
          cache_write: 0.02
        compat:
          maxTokensField: max_tokens
          supportsDeveloperRole: false
          supportsReasoningEffort: true
          supportsToolChoice: false
          thinkingFormat: deepseek
          reasoningEffortMap:
            low: low
            high: high
            xhigh: max
          extraBody:
            thinking:
              type: enabled
          requiresAssistantContentForToolCalls: true
          requiresReasoningContentForToolCalls: true
          requiresReasoningContentOnAssistantMessages: true
`)

	cfg, err := LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	provider, err := ResolveProvider(cfg, "deepseek")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v", err)
	}
	if provider.Name != "deepseek" || provider.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("provider map did not normalize name/base URL: %#v", provider)
	}
	if provider.API != "openai-completions" || provider.Auth.Type != AuthTypeAPIKey || provider.Auth.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("provider metadata/auth = %#v", provider)
	}
	if len(provider.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(provider.Models))
	}
	model := provider.Models[0]
	if model.ID != "deepseek-v4-pro" || model.ContextWindow != 128000 || !model.Reasoning {
		t.Fatalf("model metadata did not decode aliases: %#v", model)
	}
	if model.Thinking.Mode != "effort" {
		t.Fatalf("thinking mode = %q, want effort", model.Thinking.Mode)
	}
	if model.Compat.MaxTokensField != "max_tokens" || model.Compat.SupportsToolChoice == nil || *model.Compat.SupportsToolChoice {
		t.Fatalf("model compat did not decode aliases: %#v", model.Compat)
	}
	if model.Compat.SupportsDeveloperRole == nil || *model.Compat.SupportsDeveloperRole || !model.Compat.SupportsReasoningEffort {
		t.Fatalf("developer/reasoning effort compat did not decode aliases: %#v", model.Compat)
	}
	if model.Compat.ReasoningEffortMap["xhigh"] != "max" {
		t.Fatalf("reasoning effort map = %#v, want xhigh=max", model.Compat.ReasoningEffortMap)
	}
	if model.Compat.ExtraBody["thinking"] == nil ||
		!model.Compat.RequiresAssistantContentForToolCalls ||
		!model.Compat.RequiresReasoningContentForToolCalls ||
		!model.Compat.RequiresReasoningContentOnAssistantMessages {
		t.Fatalf("model compat extra body/requirements = %#v", model.Compat)
	}
}

func TestLoadConfigRejectsTopLevelLiteralAPIKey(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: deepseek
providers:
  deepseek:
    baseUrl: https://api.deepseek.com
    apiKey: literal-secret
`)

	_, err := LoadConfigFrom(projectDir)
	if err == nil {
		t.Fatal("LoadConfigFrom() error = nil, want top-level apiKey rejection")
	}
	if !containsAll(err.Error(), "top-level api_key/apiKey", "$DEEPSEEK_API_KEY", "auth.api_key") {
		t.Fatalf("error = %q, want clear top-level literal apiKey guidance", err)
	}
}

func TestLoadConfigLocalMapOverridesGlobalListProviderByName(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: deepseek
providers:
  - name: deepseek
    base_url: https://global.example
    auth:
      type: none
`)
	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `providers:
  deepseek:
    baseUrl: https://local.example
    apiKey: $DEEPSEEK_API_KEY
`)

	cfg, err := LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	provider, err := ResolveProvider(cfg, "deepseek")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v", err)
	}
	if provider.BaseURL != "https://local.example" || provider.Auth.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("local map provider did not override global list provider: %#v", provider)
	}
}

func TestLoadConfigRequiresProviders(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	_, err := LoadConfigFrom(projectDir)
	if err == nil {
		t.Fatal("LoadConfigFrom() error = nil, want missing provider error")
	}
	if got := err.Error(); !containsAll(got, filepath.Join(configDir, "config.yaml"), filepath.Join(projectDir, ".tau.yaml")) {
		t.Fatalf("error = %q, want both config paths", got)
	}
}

func TestValidateAuthTypes(t *testing.T) {
	cfg := Config{Providers: []ProviderConfig{
		{
			Name:    "example-api",
			BaseURL: "https://provider.example",
			Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKeyEnv: "EXAMPLE_PROVIDER_TOKEN"},
		},
		{
			Name:    "local",
			BaseURL: "http://localhost:11434",
			Auth:    AuthConfig{Type: AuthTypeNone},
		},
		{
			Name:    "browser",
			BaseURL: "https://browser.example",
			Auth: AuthConfig{
				Type:         AuthTypeOAuthPKCE,
				AuthorizeURL: "https://auth.example/authorize",
				TokenURL:     "https://auth.example/token",
				ClientID:     "tau",
			},
		},
	}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSyncConfigSchemaAddsMissingBlocksPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `default_provider: acme
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
ui:
  show_reasoning: true
`)

	if err := syncConfigSchema(path); err != nil {
		t.Fatalf("syncConfigSchema() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !containsAll(string(out), "metrics:", "registry:") {
		t.Fatalf("expected missing schema blocks to be added, got:\n%s", out)
	}

	// The existing ui.show_reasoning value must survive untouched.
	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !cfg.UI.ShowReasoning {
		t.Fatalf("expected ui.show_reasoning to remain true, got %+v", cfg.UI)
	}
	if cfg.DefaultProvider != "acme" {
		t.Fatalf("expected default_provider to remain acme, got %q", cfg.DefaultProvider)
	}
}

func TestSyncConfigSchemaNoopWhenAllBlocksPresentAndMetricsDirSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `default_provider: acme
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
ui:
  show_reasoning: false
registry:
  url: https://registry.example
  token: ""
metrics:
  dir: /custom/metrics/path
  session: false
  tui: false
`
	writeFile(t, path, original)

	if err := syncConfigSchema(path); err != nil {
		t.Fatalf("syncConfigSchema() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(out) != original {
		t.Fatalf("expected file to be left untouched when metrics.dir is already set, got:\n%s", out)
	}
}

// TestSyncConfigSchemaBackfillsEmptyMetricsDir guards against a real gap: an
// empty metrics.dir in an existing config almost always means the "metrics:"
// block was auto-added by an earlier run of syncConfigSchema itself (back
// when this field had no non-zero default), not a user deliberately opting
// out of metrics export — so it must be healed to MetricsDir() rather than
// left silently disabling every metrics.jsonl consumer forever.
func TestSyncConfigSchemaBackfillsEmptyMetricsDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `default_provider: acme
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
ui:
  show_reasoning: false
registry:
  url: https://registry.example
  token: ""
metrics:
  dir: ""
  session: false
  tui: false
`)

	if err := syncConfigSchema(path); err != nil {
		t.Fatalf("syncConfigSchema() error = %v", err)
	}

	var cfg Config
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.Metrics.Dir == "" {
		t.Fatal("expected metrics.dir to be backfilled to a non-empty default")
	}
	if cfg.Metrics.Dir != MetricsDir() {
		t.Fatalf("metrics.dir = %q, want %q", cfg.Metrics.Dir, MetricsDir())
	}
	// Unrelated existing values must survive untouched.
	if cfg.DefaultProvider != "acme" {
		t.Fatalf("expected default_provider to remain acme, got %q", cfg.DefaultProvider)
	}
}

func TestSyncConfigSchemaSkipsMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := syncConfigSchema(path); err != nil {
		t.Fatalf("syncConfigSchema() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected syncConfigSchema not to create a file, stat err = %v", err)
	}
}

func TestLoadConfigFromSyncsGlobalSchemaOnLoad(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)
	globalPath := filepath.Join(configDir, "config.yaml")

	writeFile(t, globalPath, `default_provider: acme
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
`)

	if _, err := LoadConfigFrom(projectDir); err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}

	out, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !containsAll(string(out), "metrics:", "registry:", "ui:") {
		t.Fatalf("expected LoadConfigFrom to sync missing schema blocks into global config, got:\n%s", out)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
