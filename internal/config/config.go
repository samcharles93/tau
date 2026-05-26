package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const localConfigName = ".tau.yaml"

// Config holds Tau user preferences loaded from global and project config files.
type Config struct {
	DefaultProvider string           `yaml:"default_provider"`
	DefaultModel    string           `yaml:"default_model"`
	Providers       []ProviderConfig `yaml:"providers"`
	Extensions      ExtensionConfig  `yaml:"extensions"`
}

// ExtensionConfig controls Tau extension discovery and loading.
type ExtensionConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	Paths        []string `yaml:"paths" json:"paths"`
	Disabled     []string `yaml:"disabled" json:"disabled"`
	AllowProject bool     `yaml:"allow_project" json:"allow_project"`

	enabledSet      bool
	allowProjectSet bool
}

func (c *ExtensionConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawExtensionConfig struct {
		Enabled      *bool    `yaml:"enabled"`
		Paths        []string `yaml:"paths"`
		Disabled     []string `yaml:"disabled"`
		AllowProject *bool    `yaml:"allow_project"`
	}
	var raw rawExtensionConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.Enabled != nil {
		c.Enabled = *raw.Enabled
		c.enabledSet = true
	}
	if raw.AllowProject != nil {
		c.AllowProject = *raw.AllowProject
		c.allowProjectSet = true
	}
	c.Paths = append([]string(nil), raw.Paths...)
	c.Disabled = append([]string(nil), raw.Disabled...)
	return nil
}

// ProviderConfig describes an OpenAI-compatible chat provider.
type ProviderConfig struct {
	Name    string     `yaml:"name" json:"name"`
	BaseURL string     `yaml:"base_url" json:"base_url"`
	Auth    AuthConfig `yaml:"auth" json:"auth"`
}

// AuthConfig describes how Tau obtains a bearer token for a provider.
type AuthConfig struct {
	Type         string `yaml:"type" json:"type"`
	APIKeyEnv    string `yaml:"api_key_env" json:"api_key_env,omitempty"`
	APIKey       string `yaml:"api_key" json:"api_key,omitempty"`
	AuthorizeURL string `yaml:"authorize_url" json:"authorize_url,omitempty"`
	TokenURL     string `yaml:"token_url" json:"token_url,omitempty"`
	ClientID     string `yaml:"client_id" json:"client_id,omitempty"`
	IDP          string `yaml:"idp" json:"idp,omitempty"`
}

const (
	AuthTypeAPIKey    = "api_key"
	AuthTypeNone      = "none"
	AuthTypeOAuthPKCE = "oauth_pkce"
)

// Dir returns the Tau configuration directory.
func Dir() string {
	return configDir()
}

// GlobalPath returns the global Tau configuration file path.
func GlobalPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

// LocalPath returns the project-local Tau configuration file path for cwd.
func LocalPath(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, localConfigName)
}

func configDir() string {
	if dir := os.Getenv("TAU_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tau")
}

// LoadConfig loads and merges global and project-local configuration.
func LoadConfig() (Config, error) {
	cwd, _ := os.Getwd()
	return LoadConfigFrom(cwd)
}

// LoadConfigFrom loads and merges global config and .tau.yaml from cwd.
func LoadConfigFrom(cwd string) (Config, error) {
	globalPath := GlobalPath()
	localPath := LocalPath(cwd)

	globalCfg, globalFound, err := readConfigFile(globalPath)
	if err != nil {
		return Config{}, err
	}
	localCfg, localFound, err := readConfigFile(localPath)
	if err != nil {
		return Config{}, err
	}
	cfg := mergeConfigs(globalCfg, localCfg)
	if len(cfg.Providers) == 0 {
		if !globalFound && !localFound {
			return Config{}, fmt.Errorf("no Tau providers configured; create %s or %s", globalPath, localPath)
		}
		return Config{}, fmt.Errorf("no Tau providers configured in %s or %s", globalPath, localPath)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readConfigFile(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("read config %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return Config{}, true, nil
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, true, nil
}

func mergeConfigs(globalCfg, localCfg Config) Config {
	merged := withDefaults(globalCfg)
	if strings.TrimSpace(localCfg.DefaultProvider) != "" {
		merged.DefaultProvider = localCfg.DefaultProvider
	}
	if strings.TrimSpace(localCfg.DefaultModel) != "" {
		merged.DefaultModel = localCfg.DefaultModel
	}

	providers := make(map[string]ProviderConfig, len(globalCfg.Providers)+len(localCfg.Providers))
	order := make([]string, 0, len(globalCfg.Providers)+len(localCfg.Providers))
	for _, provider := range globalCfg.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			continue
		}
		providers[name] = provider
		order = append(order, name)
	}
	for _, provider := range localCfg.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			continue
		}
		if _, exists := providers[name]; !exists {
			order = append(order, name)
		}
		providers[name] = provider
	}

	merged.Providers = make([]ProviderConfig, 0, len(order))
	for _, name := range order {
		merged.Providers = append(merged.Providers, providers[name])
	}
	merged.Extensions = mergeExtensionConfigs(merged.Extensions, localCfg.Extensions)
	return merged
}

func withDefaults(cfg Config) Config {
	if !cfg.Extensions.enabledSet {
		cfg.Extensions.Enabled = true
	}
	if !cfg.Extensions.allowProjectSet {
		cfg.Extensions.AllowProject = true
	}
	return cfg
}

func mergeExtensionConfigs(globalCfg, localCfg ExtensionConfig) ExtensionConfig {
	merged := globalCfg
	if localCfg.enabledSet {
		merged.Enabled = localCfg.Enabled
		merged.enabledSet = true
	}
	if localCfg.allowProjectSet {
		merged.AllowProject = localCfg.AllowProject
		merged.allowProjectSet = true
	}
	merged.Paths = append(append([]string(nil), globalCfg.Paths...), localCfg.Paths...)
	merged.Disabled = append(append([]string(nil), globalCfg.Disabled...), localCfg.Disabled...)
	return merged
}

// Validate checks that configured providers have the fields required by auth type.
func Validate(cfg Config) error {
	seen := make(map[string]struct{}, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			return errors.New("provider name is required")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate provider %q", name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(provider.BaseURL) == "" {
			return fmt.Errorf("provider %q base_url is required", name)
		}
		switch strings.TrimSpace(provider.Auth.Type) {
		case AuthTypeAPIKey:
			if strings.TrimSpace(provider.Auth.APIKeyEnv) == "" && strings.TrimSpace(provider.Auth.APIKey) == "" {
				return fmt.Errorf("provider %q api_key auth requires api_key_env or api_key", name)
			}
		case AuthTypeNone:
		case AuthTypeOAuthPKCE:
			if strings.TrimSpace(provider.Auth.AuthorizeURL) == "" {
				return fmt.Errorf("provider %q oauth_pkce auth requires authorize_url", name)
			}
			if strings.TrimSpace(provider.Auth.TokenURL) == "" {
				return fmt.Errorf("provider %q oauth_pkce auth requires token_url", name)
			}
			if strings.TrimSpace(provider.Auth.ClientID) == "" {
				return fmt.Errorf("provider %q oauth_pkce auth requires client_id", name)
			}
		default:
			return fmt.Errorf("provider %q has unsupported auth type %q", name, provider.Auth.Type)
		}
	}
	return nil
}

// ResolveProvider returns a provider selected by flag or default_provider.
func ResolveProvider(cfg Config, providerName string) (ProviderConfig, error) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		name = strings.TrimSpace(cfg.DefaultProvider)
	}
	if name == "" {
		return ProviderConfig{}, errors.New("provider is required; pass --provider or set default_provider")
	}
	for _, provider := range cfg.Providers {
		if provider.Name == name {
			return provider, nil
		}
	}
	return ProviderConfig{}, fmt.Errorf("unknown provider %q (available: %s)", name, strings.Join(ProviderNames(cfg), ", "))
}

// ProviderNames returns configured provider names in resolution order.
func ProviderNames(cfg Config) []string {
	names := make([]string, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if strings.TrimSpace(provider.Name) != "" {
			names = append(names, provider.Name)
		}

		// NormalizeExtensions applies extension defaults to an ExtensionConfig value.
		func NormalizeExtensions(cfg ExtensionConfig) ExtensionConfig {
			return withDefaults(Config{Extensions: cfg}).Extensions
		}
	}
	return names
}
