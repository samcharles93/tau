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
