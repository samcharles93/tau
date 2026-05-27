package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMergesGlobalAndLocal(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: global
default_model: global-model
extensions:
  paths:
    - /global/ext
  disabled:
    - global-disabled
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
extensions:
  allow_project: false
  paths:
    - /local/ext
  disabled:
    - local-disabled
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
	if !cfg.Extensions.Enabled {
		t.Fatal("Extensions.Enabled = false, want default true")
	}
	if cfg.Extensions.AllowProject {
		t.Fatal("Extensions.AllowProject = true, want local override false")
	}
	if got := strings.Join(cfg.Extensions.Paths, ","); got != "/global/ext,/local/ext" {
		t.Fatalf("Extensions.Paths = %q, want merged global/local paths", got)
	}
	if got := strings.Join(cfg.Extensions.Disabled, ","); got != "global-disabled,local-disabled" {
		t.Fatalf("Extensions.Disabled = %q, want merged global/local disabled list", got)
	}
}

func TestLoadConfigExtensionDefaultsAndOverrides(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)

	writeFile(t, filepath.Join(configDir, "config.yaml"), `default_provider: local
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
	if !cfg.Extensions.Enabled {
		t.Fatal("Extensions.Enabled default = false, want true")
	}
	if !cfg.Extensions.AllowProject {
		t.Fatal("Extensions.AllowProject default = false, want true")
	}

	writeFile(t, filepath.Join(projectDir, ".tau.yaml"), `extensions:
  enabled: false
`)
	cfg, err = LoadConfigFrom(projectDir)
	if err != nil {
		t.Fatalf("LoadConfigFrom() error = %v", err)
	}
	if cfg.Extensions.Enabled {
		t.Fatal("Extensions.Enabled = true, want local override false")
	}
	if !cfg.Extensions.AllowProject {
		t.Fatal("Extensions.AllowProject = false, want default true")
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
