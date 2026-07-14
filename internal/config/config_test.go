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

func TestConfigUnmarshalYAMLPopulatesAutoCompact(t *testing.T) {
	var cfg Config
	err := yaml.Unmarshal([]byte(`
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
auto_compact:
  enabled: true
  threshold_ratio: 0.8
  targetRatio: 0.25
  model: compact-model
`), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !cfg.AutoCompact.Enabled {
		t.Fatal("AutoCompact.Enabled = false, want true")
	}
	if cfg.AutoCompact.ThresholdRatio != 0.8 {
		t.Fatalf("AutoCompact.ThresholdRatio = %v, want 0.8", cfg.AutoCompact.ThresholdRatio)
	}
	if cfg.AutoCompact.TargetRatio != 0.25 {
		t.Fatalf("AutoCompact.TargetRatio = %v, want 0.25", cfg.AutoCompact.TargetRatio)
	}
	if cfg.AutoCompact.Model != "compact-model" {
		t.Fatalf("AutoCompact.Model = %q, want compact-model", cfg.AutoCompact.Model)
	}
}

func TestLoadConfigAutoCompactLocalCanDisableGlobal(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: acme
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
auto_compact:
  enabled: true
  threshold_ratio: 0.8
`)
	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `auto_compact:
  enabled: false
  target_ratio: 0.25
`)

	cfg, err := LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	if cfg.AutoCompact.Enabled {
		t.Fatal("AutoCompact.Enabled = true, want local false override")
	}
	if cfg.AutoCompact.ThresholdRatio != 0.8 {
		t.Fatalf("AutoCompact.ThresholdRatio = %v, want inherited 0.8", cfg.AutoCompact.ThresholdRatio)
	}
	if cfg.AutoCompact.TargetRatio != 0.25 {
		t.Fatalf("AutoCompact.TargetRatio = %v, want local 0.25", cfg.AutoCompact.TargetRatio)
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
	if !cfg.UI.ShowReasoning {
		t.Fatal("UI.ShowReasoning default = false, want true")
	}
}

func TestLoadConfigUIToolCallsDefaultCollapsedDefaultAndLocalOverride(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: local
ui:
  tool_calls_default_collapsed: true
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
	if !cfg.UI.ToolCallsDefaultCollapsed {
		t.Fatal("UI.ToolCallsDefaultCollapsed = false, want global true")
	}

	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `ui:
  tool_calls_default_collapsed: false
`)
	cfg, err = LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() with local override error = %v", err)
	}
	if cfg.UI.ToolCallsDefaultCollapsed {
		t.Fatal("UI.ToolCallsDefaultCollapsed = true, want local override false")
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
	if cfg.UI.ToolCallsDefaultCollapsed {
		t.Fatal("UI.ToolCallsDefaultCollapsed default = true, want false")
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
auto_compact:
  enabled: false
agents:
  default_max_depth: 0
  default_max_turns: 0
  default_timeout: 0s
  depth_ceiling: 0
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

// --- ResolveModelMode ---

// TestResolveModelMode_InheritedProviderWithoutModelFallsThroughToDefault
// proves an inherited provider with no inherited model does NOT win over
// the config defaults — a non-empty provider paired with an empty model
// would otherwise produce an unusable empty model reference downstream
// (this exact case broke agent-tool spawns: the provider is populated
// from the session's already-selected provider, but no model override was
// ever given).
func TestResolveModelMode_InheritedProviderWithoutModelFallsThroughToDefault(t *testing.T) {
	provider, model := ResolveModelMode(
		"", "", "", // no spawn override, no spec model/provider
		"openai", "", // inherited: provider set, model NOT set
		"anthropic", "claude-default", // defaults
		nil,
	)
	if model != "claude-default" {
		t.Errorf("model = %q, want the default model (inherited model was empty)", model)
	}
	if provider != "anthropic" {
		t.Errorf("provider = %q, want the default provider", provider)
	}
}

// TestResolveModelMode_InheritedPairUsedWhenBothPresent proves a genuinely
// usable inherited pair still wins over defaults.
func TestResolveModelMode_InheritedPairUsedWhenBothPresent(t *testing.T) {
	provider, model := ResolveModelMode(
		"", "", "",
		"openai", "gpt-5.6-luna",
		"anthropic", "claude-default",
		nil,
	)
	if provider != "openai" || model != "gpt-5.6-luna" {
		t.Errorf("got (%q, %q), want the inherited pair (openai, gpt-5.6-luna)", provider, model)
	}
}

// TestResolveModelMode_SpawnOverrideWins proves the spawn-call parameter
// still takes precedence over everything else, tier or concrete.
func TestResolveModelMode_SpawnOverrideWins(t *testing.T) {
	provider, model := ResolveModelMode(
		"fast", "", "",
		"openai", "gpt-5.6-luna",
		"anthropic", "claude-default",
		map[string]ModeConfig{"fast": {Provider: "groq", Model: "llama-fast"}},
	)
	if provider != "groq" || model != "llama-fast" {
		t.Errorf("got (%q, %q), want the tier-mapped pair (groq, llama-fast)", provider, model)
	}
}

// --- mergeModelModes ---

// TestMergeModelModes_NilGlobalWithLocalEntriesDoesNotPanic proves a
// project-local model_modes entry doesn't crash when the global config has
// none at all (normalizeModelModesKeys(nil) returns nil, and assigning
// into that nil map previously panicked).
func TestMergeModelModes_NilGlobalWithLocalEntriesDoesNotPanic(t *testing.T) {
	merged := mergeModelModes(nil, map[string]ModeConfig{
		"fast": {Provider: "openai", Model: "gpt-5.6-luna"},
	})
	if merged["fast"].Model != "gpt-5.6-luna" {
		t.Errorf("merged[fast].Model = %q, want gpt-5.6-luna", merged["fast"].Model)
	}
}

func TestMergeModelModes_BothNilReturnsNil(t *testing.T) {
	if merged := mergeModelModes(nil, nil); merged != nil {
		t.Errorf("expected nil, got %v", merged)
	}
}

func TestMergeModelModes_LocalOverridesGlobal(t *testing.T) {
	merged := mergeModelModes(
		map[string]ModeConfig{"fast": {Provider: "groq", Model: "llama"}},
		map[string]ModeConfig{"fast": {Provider: "openai", Model: "gpt-5.6-luna"}},
	)
	if merged["fast"].Provider != "openai" {
		t.Errorf("local should override global, got provider=%q", merged["fast"].Provider)
	}
}

// --- SaveDefaultProviderAndModel ---

// TestSaveDefaultProviderAndModelPreservesCommentsAndFormatting guards
// against the original bug: SaveDefaultProviderAndModel used to round-trip
// the whole file through a map[string]any, which discards comments, key
// order, and every YAML formatting choice. Setup writes defaults during
// first-run onboarding, so destroying a user's hand-edited config there is
// unacceptable.
func TestSaveDefaultProviderAndModelPreservesCommentsAndFormatting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()
	writeFile(t, path, `# my personal tau config, hand-tuned
default_provider: acme # was the cheapest option

providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none

ui:
  show_reasoning: true # I like seeing the thinking
`)

	if err := SaveDefaultProviderAndModel("", "openrouter", "gpt-5.6"); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(out)
	if !containsAll(
		got,
		"# my personal tau config, hand-tuned",
		"# was the cheapest option",
		"# I like seeing the thinking",
		"base_url: https://acme.example",
	) {
		t.Fatalf("expected comments and other fields to survive, got:\n%s", got)
	}

	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.DefaultProvider != "openrouter" {
		t.Fatalf("DefaultProvider = %q, want openrouter", cfg.DefaultProvider)
	}
	if cfg.DefaultModel != "gpt-5.6" {
		t.Fatalf("DefaultModel = %q, want gpt-5.6", cfg.DefaultModel)
	}
	if !cfg.UI.ShowReasoning {
		t.Fatal("expected ui.show_reasoning to remain true")
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "acme" {
		t.Fatalf("expected the acme provider to survive untouched, got %+v", cfg.Providers)
	}
}

// TestSaveDefaultProviderAndModelUpdatesOnlyTargetKeys guards against
// inserting a duplicate default_provider/default_model pair alongside an
// existing one instead of updating in place.
func TestSaveDefaultProviderAndModelUpdatesOnlyTargetKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()
	writeFile(t, path, `default_provider: acme
default_model: acme-fast
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
`)

	if err := SaveDefaultProviderAndModel("", "openrouter", "gpt-5.6"); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if n := strings.Count(string(out), "default_provider:"); n != 1 {
		t.Fatalf("expected exactly one default_provider key, got %d in:\n%s", n, out)
	}
	if n := strings.Count(string(out), "default_model:"); n != 1 {
		t.Fatalf("expected exactly one default_model key, got %d in:\n%s", n, out)
	}

	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.DefaultProvider != "openrouter" || cfg.DefaultModel != "gpt-5.6" {
		t.Fatalf("got provider=%q model=%q, want openrouter/gpt-5.6", cfg.DefaultProvider, cfg.DefaultModel)
	}
}

func TestSaveDefaultProviderAndModelCreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no config file yet, stat err = %v", err)
	}

	if err := SaveDefaultProviderAndModel("", "openrouter", "gpt-5.6"); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}

	var cfg Config
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.DefaultProvider != "openrouter" || cfg.DefaultModel != "gpt-5.6" {
		t.Fatalf("got provider=%q model=%q, want openrouter/gpt-5.6", cfg.DefaultProvider, cfg.DefaultModel)
	}
}

// TestSaveDefaultProviderAndModelAddsKeysToConfigWithoutThem guards the
// fourth acceptance criterion: an existing config with no default_provider/
// default_model yet gets them appended, not rejected or ignored.
func TestSaveDefaultProviderAndModelAddsKeysToConfigWithoutThem(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()
	writeFile(t, path, `providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
`)

	if err := SaveDefaultProviderAndModel("", "openrouter", "gpt-5.6"); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.DefaultProvider != "openrouter" || cfg.DefaultModel != "gpt-5.6" {
		t.Fatalf("got provider=%q model=%q, want openrouter/gpt-5.6", cfg.DefaultProvider, cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "acme" {
		t.Fatalf("expected the acme provider to survive untouched, got %+v", cfg.Providers)
	}
}

func TestSaveDefaultProviderAndModelEmptyArgsIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()

	if err := SaveDefaultProviderAndModel("", "", ""); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no file to be created for empty provider/model, stat err = %v", err)
	}
}

func TestSaveDefaultProviderAndModelPartialUpdateLeavesOtherKeyAlone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", dir)
	path := GlobalPath()
	writeFile(t, path, `default_provider: acme
default_model: acme-fast
`)

	if err := SaveDefaultProviderAndModel("", "", "gpt-5.6"); err != nil {
		t.Fatalf("SaveDefaultProviderAndModel() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if cfg.DefaultProvider != "acme" {
		t.Fatalf("DefaultProvider = %q, want it left as acme when provider arg is empty", cfg.DefaultProvider)
	}
	if cfg.DefaultModel != "gpt-5.6" {
		t.Fatalf("DefaultModel = %q, want gpt-5.6", cfg.DefaultModel)
	}
}
