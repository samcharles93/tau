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
	UI              UIConfig         `yaml:"ui"`
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	type rawConfig struct {
		DefaultProvider string          `yaml:"default_provider"`
		DefaultModel    string          `yaml:"default_model"`
		Providers       yaml.Node       `yaml:"providers"`
		Extensions      ExtensionConfig `yaml:"extensions"`
		UI              UIConfig        `yaml:"ui"`
	}
	var raw rawConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.DefaultProvider = raw.DefaultProvider
	c.DefaultModel = raw.DefaultModel
	c.Extensions = raw.Extensions
	c.UI = raw.UI
	if raw.Providers.Kind != 0 {
		providers, err := decodeProviders(raw.Providers)
		if err != nil {
			return err
		}
		c.Providers = providers
	}
	return nil
}

// UIConfig controls terminal UI observability and presentation.
type UIConfig struct {
	ShowReasoning bool `yaml:"show_reasoning" json:"show_reasoning"`

	showReasoningSet bool
}

func (c *UIConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawUIConfig struct {
		ShowReasoning *bool `yaml:"show_reasoning"`
	}
	var raw rawUIConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.ShowReasoning != nil {
		c.ShowReasoning = *raw.ShowReasoning
		c.showReasoningSet = true
	}
	return nil
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
	Name    string        `yaml:"name" json:"name"`
	Type    string        `yaml:"type,omitempty" json:"type,omitempty"`
	API     string        `yaml:"api,omitempty" json:"api,omitempty"`
	BaseURL string        `yaml:"base_url" json:"base_url"`
	Auth    AuthConfig    `yaml:"auth" json:"auth"`
	Models  []ModelConfig `yaml:"models,omitempty" json:"models,omitempty"`
}

func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawProviderConfig struct {
		Name         string       `yaml:"name"`
		Type         string       `yaml:"type"`
		API          string       `yaml:"api"`
		BaseURL      string       `yaml:"base_url"`
		BaseURLCamel string       `yaml:"baseUrl"`
		Auth         AuthConfig   `yaml:"auth"`
		APIKey       string       `yaml:"api_key"`
		APIKeyCamel  string       `yaml:"apiKey"`
		APIKeyEnv    string       `yaml:"api_key_env"`
		Models       modelConfigs `yaml:"models"`
	}
	var raw rawProviderConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	p.Name = raw.Name
	p.Type = raw.Type
	p.API = raw.API
	p.BaseURL = firstNonEmpty(raw.BaseURL, raw.BaseURLCamel)
	p.Auth = raw.Auth
	p.Models = []ModelConfig(raw.Models)

	apiKey := firstNonEmpty(raw.APIKey, raw.APIKeyCamel)
	if strings.TrimSpace(apiKey) != "" {
		env, ok := envReference(apiKey)
		if !ok {
			return errors.New("top-level api_key/apiKey must reference an environment variable like $DEEPSEEK_API_KEY; use auth.api_key for literal secrets")
		}
		if strings.TrimSpace(p.Auth.Type) == "" {
			p.Auth.Type = AuthTypeAPIKey
		}
		p.Auth.APIKeyEnv = env
	}
	if strings.TrimSpace(raw.APIKeyEnv) != "" {
		if strings.TrimSpace(p.Auth.Type) == "" {
			p.Auth.Type = AuthTypeAPIKey
		}
		p.Auth.APIKeyEnv = raw.APIKeyEnv
	}
	return nil
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

func (a *AuthConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawAuthConfig struct {
		Type              string `yaml:"type"`
		APIKeyEnv         string `yaml:"api_key_env"`
		APIKeyEnvCamel    string `yaml:"apiKeyEnv"`
		APIKey            string `yaml:"api_key"`
		APIKeyCamel       string `yaml:"apiKey"`
		AuthorizeURL      string `yaml:"authorize_url"`
		AuthorizeURLCamel string `yaml:"authorizeUrl"`
		TokenURL          string `yaml:"token_url"`
		TokenURLCamel     string `yaml:"tokenUrl"`
		ClientID          string `yaml:"client_id"`
		ClientIDCamel     string `yaml:"clientId"`
		IDP               string `yaml:"idp"`
	}
	var raw rawAuthConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	a.Type = raw.Type
	a.APIKeyEnv = firstNonEmpty(raw.APIKeyEnv, raw.APIKeyEnvCamel)
	a.APIKey = firstNonEmpty(raw.APIKey, raw.APIKeyCamel)
	a.AuthorizeURL = firstNonEmpty(raw.AuthorizeURL, raw.AuthorizeURLCamel)
	a.TokenURL = firstNonEmpty(raw.TokenURL, raw.TokenURLCamel)
	a.ClientID = firstNonEmpty(raw.ClientID, raw.ClientIDCamel)
	a.IDP = raw.IDP
	return nil
}

// ModelConfig stores optional configured model metadata for a provider.
type ModelConfig struct {
	ID               string         `yaml:"id" json:"id"`
	Name             string         `yaml:"name,omitempty" json:"name,omitempty"`
	ContextWindow    int            `yaml:"context_window,omitempty" json:"context_window,omitempty"`
	DefaultMaxTokens int            `yaml:"default_max_tokens,omitempty" json:"default_max_tokens,omitempty"`
	MaxTokens        int            `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Input            []string       `yaml:"input,omitempty" json:"input,omitempty"`
	Reasoning        bool           `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	Thinking         ThinkingConfig `yaml:"thinking,omitempty" json:"thinking,omitempty"`
	Cost             CostConfig     `yaml:"cost,omitempty" json:"cost,omitempty"`
	Compat           CompatConfig   `yaml:"compat,omitempty" json:"compat,omitempty"`
}

func (m *ModelConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawModelConfig struct {
		ID                                               string            `yaml:"id"`
		Name                                             string            `yaml:"name"`
		ContextWindow                                    int               `yaml:"context_window"`
		ContextWindowCamel                               int               `yaml:"contextWindow"`
		DefaultMaxTokens                                 int               `yaml:"default_max_tokens"`
		DefaultMaxTokensCamel                            int               `yaml:"defaultMaxTokens"`
		MaxTokens                                        int               `yaml:"max_tokens"`
		MaxTokensCamel                                   int               `yaml:"maxTokens"`
		Input                                            []string          `yaml:"input"`
		Reasoning                                        bool              `yaml:"reasoning"`
		CanReason                                        bool              `yaml:"can_reason"`
		CanReasonCamel                                   bool              `yaml:"canReason"`
		Thinking                                         ThinkingConfig    `yaml:"thinking"`
		Cost                                             CostConfig        `yaml:"cost"`
		Compat                                           CompatConfig      `yaml:"compat"`
		MaxTokensField                                   string            `yaml:"max_tokens_field"`
		MaxTokensFieldCamel                              string            `yaml:"maxTokensField"`
		SupportsToolChoice                               *bool             `yaml:"supports_tool_choice"`
		SupportsToolChoiceCamel                          *bool             `yaml:"supportsToolChoice"`
		RequiresAssistantContentForToolCalls             bool              `yaml:"requires_assistant_content_for_tool_calls"`
		RequiresAssistantContentForToolCallsCamel        bool              `yaml:"requiresAssistantContentForToolCalls"`
		RequiresReasoningContentForToolCalls             bool              `yaml:"requires_reasoning_content_for_tool_calls"`
		RequiresReasoningContentForToolCallsCamel        bool              `yaml:"requiresReasoningContentForToolCalls"`
		RequiresReasoningContentOnAssistantMessages      bool              `yaml:"requires_reasoning_content_on_assistant_messages"`
		RequiresReasoningContentOnAssistantMessagesCamel bool              `yaml:"requiresReasoningContentOnAssistantMessages"`
		ThinkingFormat                                   string            `yaml:"thinking_format"`
		ThinkingFormatCamel                              string            `yaml:"thinkingFormat"`
		ReasoningEffortMap                               map[string]string `yaml:"reasoning_effort_map"`
		ReasoningEffortMapCamel                          map[string]string `yaml:"reasoningEffortMap"`
		ExtraBody                                        map[string]any    `yaml:"extra_body"`
		ExtraBodyCamel                                   map[string]any    `yaml:"extraBody"`
	}
	var raw rawModelConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.Name = raw.Name
	m.ContextWindow = firstNonZero(raw.ContextWindow, raw.ContextWindowCamel)
	m.DefaultMaxTokens = firstNonZero(raw.DefaultMaxTokens, raw.DefaultMaxTokensCamel)
	m.MaxTokens = firstNonZero(raw.MaxTokens, raw.MaxTokensCamel)
	m.Input = append([]string(nil), raw.Input...)
	m.Reasoning = raw.Reasoning || raw.CanReason || raw.CanReasonCamel
	m.Thinking = raw.Thinking
	m.Cost = raw.Cost
	m.Compat = raw.Compat
	if field := firstNonEmpty(raw.MaxTokensField, raw.MaxTokensFieldCamel); field != "" {
		m.Compat.MaxTokensField = field
	}
	if supportsToolChoice := firstNonNilBool(raw.SupportsToolChoice, raw.SupportsToolChoiceCamel); supportsToolChoice != nil {
		m.Compat.SupportsToolChoice = supportsToolChoice
	}
	m.Compat.RequiresAssistantContentForToolCalls = m.Compat.RequiresAssistantContentForToolCalls ||
		raw.RequiresAssistantContentForToolCalls ||
		raw.RequiresAssistantContentForToolCallsCamel
	m.Compat.RequiresReasoningContentForToolCalls = m.Compat.RequiresReasoningContentForToolCalls ||
		raw.RequiresReasoningContentForToolCalls ||
		raw.RequiresReasoningContentForToolCallsCamel
	m.Compat.RequiresReasoningContentOnAssistantMessages = m.Compat.RequiresReasoningContentOnAssistantMessages ||
		raw.RequiresReasoningContentOnAssistantMessages ||
		raw.RequiresReasoningContentOnAssistantMessagesCamel
	if format := firstNonEmpty(raw.ThinkingFormat, raw.ThinkingFormatCamel); format != "" {
		m.Compat.ThinkingFormat = format
	}
	if effortMap := firstNonNilStringMap(raw.ReasoningEffortMap, raw.ReasoningEffortMapCamel); effortMap != nil {
		m.Compat.ReasoningEffortMap = effortMap
	}
	if extraBody := firstNonNilAnyMap(raw.ExtraBody, raw.ExtraBodyCamel); extraBody != nil {
		m.Compat.ExtraBody = extraBody
	}
	return nil
}

// ThinkingConfig stores provider-specific thinking controls without applying them.
type ThinkingConfig struct {
	MinLevel string `yaml:"min_level,omitempty" json:"min_level,omitempty"`
	MaxLevel string `yaml:"max_level,omitempty" json:"max_level,omitempty"`
	Mode     string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

func (t *ThinkingConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawThinkingConfig struct {
		MinLevel      string `yaml:"min_level"`
		MinLevelCamel string `yaml:"minLevel"`
		MaxLevel      string `yaml:"max_level"`
		MaxLevelCamel string `yaml:"maxLevel"`
		Mode          string `yaml:"mode"`
	}
	var raw rawThinkingConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	t.MinLevel = firstNonEmpty(raw.MinLevel, raw.MinLevelCamel)
	t.MaxLevel = firstNonEmpty(raw.MaxLevel, raw.MaxLevelCamel)
	t.Mode = raw.Mode
	return nil
}

// CostConfig stores optional per-token pricing metadata.
type CostConfig struct {
	Input      float64 `yaml:"input,omitempty" json:"input,omitempty"`
	Output     float64 `yaml:"output,omitempty" json:"output,omitempty"`
	CacheRead  float64 `yaml:"cache_read,omitempty" json:"cache_read,omitempty"`
	CacheWrite float64 `yaml:"cache_write,omitempty" json:"cache_write,omitempty"`
}

func (c *CostConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawCostConfig struct {
		Input           float64 `yaml:"input"`
		Output          float64 `yaml:"output"`
		CacheRead       float64 `yaml:"cache_read"`
		CacheReadCamel  float64 `yaml:"cacheRead"`
		CacheWrite      float64 `yaml:"cache_write"`
		CacheWriteCamel float64 `yaml:"cacheWrite"`
	}
	var raw rawCostConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.Input = raw.Input
	c.Output = raw.Output
	c.CacheRead = firstNonZeroFloat(raw.CacheRead, raw.CacheReadCamel)
	c.CacheWrite = firstNonZeroFloat(raw.CacheWrite, raw.CacheWriteCamel)
	return nil
}

// CompatConfig stores provider/model compatibility metadata for request shaping.
type CompatConfig struct {
	MaxTokensField                              string            `yaml:"max_tokens_field,omitempty" json:"max_tokens_field,omitempty"`
	SupportsDeveloperRole                       *bool             `yaml:"supports_developer_role,omitempty" json:"supports_developer_role,omitempty"`
	SupportsReasoningEffort                     bool              `yaml:"supports_reasoning_effort,omitempty" json:"supports_reasoning_effort,omitempty"`
	SupportsToolChoice                          *bool             `yaml:"supports_tool_choice,omitempty" json:"supports_tool_choice,omitempty"`
	RequiresAssistantContentForToolCalls        bool              `yaml:"requires_assistant_content_for_tool_calls,omitempty" json:"requires_assistant_content_for_tool_calls,omitempty"`
	RequiresReasoningContentForToolCalls        bool              `yaml:"requires_reasoning_content_for_tool_calls,omitempty" json:"requires_reasoning_content_for_tool_calls,omitempty"`
	RequiresReasoningContentOnAssistantMessages bool              `yaml:"requires_reasoning_content_on_assistant_messages,omitempty" json:"requires_reasoning_content_on_assistant_messages,omitempty"`
	ThinkingFormat                              string            `yaml:"thinking_format,omitempty" json:"thinking_format,omitempty"`
	ReasoningEffortMap                          map[string]string `yaml:"reasoning_effort_map,omitempty" json:"reasoning_effort_map,omitempty"`
	ExtraBody                                   map[string]any    `yaml:"extra_body,omitempty" json:"extra_body,omitempty"`
}

func (c *CompatConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawCompatConfig struct {
		MaxTokensField                                   string            `yaml:"max_tokens_field"`
		MaxTokensFieldCamel                              string            `yaml:"maxTokensField"`
		SupportsDeveloperRole                            *bool             `yaml:"supports_developer_role"`
		SupportsDeveloperRoleCamel                       *bool             `yaml:"supportsDeveloperRole"`
		SupportsReasoningEffort                          bool              `yaml:"supports_reasoning_effort"`
		SupportsReasoningEffortCamel                     bool              `yaml:"supportsReasoningEffort"`
		SupportsToolChoice                               *bool             `yaml:"supports_tool_choice"`
		SupportsToolChoiceCamel                          *bool             `yaml:"supportsToolChoice"`
		RequiresAssistantContentForToolCalls             bool              `yaml:"requires_assistant_content_for_tool_calls"`
		RequiresAssistantContentForToolCallsCamel        bool              `yaml:"requiresAssistantContentForToolCalls"`
		RequiresReasoningContentForToolCalls             bool              `yaml:"requires_reasoning_content_for_tool_calls"`
		RequiresReasoningContentForToolCallsCamel        bool              `yaml:"requiresReasoningContentForToolCalls"`
		RequiresReasoningContentOnAssistantMessages      bool              `yaml:"requires_reasoning_content_on_assistant_messages"`
		RequiresReasoningContentOnAssistantMessagesCamel bool              `yaml:"requiresReasoningContentOnAssistantMessages"`
		ThinkingFormat                                   string            `yaml:"thinking_format"`
		ThinkingFormatCamel                              string            `yaml:"thinkingFormat"`
		ReasoningEffortMap                               map[string]string `yaml:"reasoning_effort_map"`
		ReasoningEffortMapCamel                          map[string]string `yaml:"reasoningEffortMap"`
		ExtraBody                                        map[string]any    `yaml:"extra_body"`
		ExtraBodyCamel                                   map[string]any    `yaml:"extraBody"`
	}
	var raw rawCompatConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.MaxTokensField = firstNonEmpty(raw.MaxTokensField, raw.MaxTokensFieldCamel)
	c.SupportsDeveloperRole = firstNonNilBool(raw.SupportsDeveloperRole, raw.SupportsDeveloperRoleCamel)
	c.SupportsReasoningEffort = raw.SupportsReasoningEffort || raw.SupportsReasoningEffortCamel
	c.SupportsToolChoice = firstNonNilBool(raw.SupportsToolChoice, raw.SupportsToolChoiceCamel)
	c.RequiresAssistantContentForToolCalls = raw.RequiresAssistantContentForToolCalls || raw.RequiresAssistantContentForToolCallsCamel
	c.RequiresReasoningContentForToolCalls = raw.RequiresReasoningContentForToolCalls || raw.RequiresReasoningContentForToolCallsCamel
	c.RequiresReasoningContentOnAssistantMessages = raw.RequiresReasoningContentOnAssistantMessages || raw.RequiresReasoningContentOnAssistantMessagesCamel
	c.ThinkingFormat = firstNonEmpty(raw.ThinkingFormat, raw.ThinkingFormatCamel)
	c.ReasoningEffortMap = firstNonNilStringMap(raw.ReasoningEffortMap, raw.ReasoningEffortMapCamel)
	c.ExtraBody = firstNonNilAnyMap(raw.ExtraBody, raw.ExtraBodyCamel)
	return nil
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

type modelConfigs []ModelConfig

func (m *modelConfigs) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var models []ModelConfig
		if err := value.Decode(&models); err != nil {
			return err
		}
		*m = models
		return nil
	case yaml.MappingNode:
		models := make([]ModelConfig, 0, len(value.Content)/2)
		for i := 0; i+1 < len(value.Content); i += 2 {
			key := strings.TrimSpace(value.Content[i].Value)
			var model ModelConfig
			if err := value.Content[i+1].Decode(&model); err != nil {
				return err
			}
			if strings.TrimSpace(model.ID) == "" {
				model.ID = key
			}
			models = append(models, model)
		}
		*m = models
		return nil
	case 0:
		return nil
	default:
		return fmt.Errorf("providers.models must be a list or map")
	}
}

func decodeProviders(node yaml.Node) ([]ProviderConfig, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		var providers []ProviderConfig
		if err := node.Decode(&providers); err != nil {
			return nil, err
		}
		return providers, nil
	case yaml.MappingNode:
		providers := make([]ProviderConfig, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.TrimSpace(node.Content[i].Value)
			var provider ProviderConfig
			if err := node.Content[i+1].Decode(&provider); err != nil {
				return nil, err
			}
			if strings.TrimSpace(provider.Name) == "" {
				provider.Name = key
			}
			providers = append(providers, provider)
		}
		return providers, nil
	default:
		return nil, fmt.Errorf("providers must be a list or map")
	}
}

func envReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "$") {
		return "", false
	}
	env := strings.TrimPrefix(value, "$")
	env = strings.TrimPrefix(env, "{")
	env = strings.TrimSuffix(env, "}")
	if strings.TrimSpace(env) == "" {
		return "", false
	}
	return env, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonNilBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonNilStringMap(values ...map[string]string) map[string]string {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonNilAnyMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
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
	merged.UI = mergeUIConfigs(merged.UI, localCfg.UI)
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

func mergeUIConfigs(globalCfg, localCfg UIConfig) UIConfig {
	merged := globalCfg
	if localCfg.showReasoningSet {
		merged.ShowReasoning = localCfg.ShowReasoning
		merged.showReasoningSet = true
	}
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
	}
	return names
}

// NormalizeExtensions applies extension defaults to an ExtensionConfig value.
func NormalizeExtensions(cfg ExtensionConfig) ExtensionConfig {
	return withDefaults(Config{Extensions: cfg}).Extensions
}
