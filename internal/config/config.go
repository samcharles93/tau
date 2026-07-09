package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const localConfigName = ".tau.yaml"

// Config holds Tau user preferences loaded from global and project config files.
type Config struct {
	DefaultProvider string           `yaml:"default_provider"`
	DefaultModel    string           `yaml:"default_model"`
	Providers       []ProviderConfig `yaml:"providers"`
	UI              UIConfig         `yaml:"ui"`
	Debug           bool             `yaml:"debug"`
	// Registry configures the plugin registry connection.
	Registry RegistryConfig `yaml:"registry"`
	// Plugins holds per-plugin config blocks (`plugins.<name>:`), passed through
	// to plugins via the HostService.GetConfig reverse RPC.
	Plugins map[string]map[string]any `yaml:"plugins"`
	// DisabledSkills lists skill names to exclude from the active catalog.
	DisabledSkills []string `yaml:"disabled_skills,omitempty"`
	// SkillPaths lists additional directories to scan for skills.
	SkillPaths []string `yaml:"skill_paths,omitempty"`
	// Metrics configures observability export and tracking.
	Metrics     MetricsConfig     `yaml:"metrics"`
	AutoCompact AutoCompactConfig `yaml:"auto_compact"`
}

// MetricsConfig controls observability export and session cost tracking.
type MetricsConfig struct {
	// Dir is the path where metrics JSONL files are written. Empty disables
	// file export. syncConfigSchema backfills this to MetricsDir() the first
	// time it finds it missing/empty in an existing config file, so file
	// export is on by default — set it explicitly to "" to opt back out.
	Dir string `yaml:"dir"`
	// Session enables always-on per-session cost and token aggregation.
	// When true, session summaries include cost metadata.
	Session bool `yaml:"session"`
	// TUI enables the TUI cost status bar widget. Ignored in headless mode.
	TUI bool `yaml:"tui"`
}

// AutoCompactConfig controls automatic conversation-history compaction before
// an LLM request when the active session approaches the model context window.
type AutoCompactConfig struct {
	Enabled        bool    `yaml:"enabled"`
	ThresholdRatio float64 `yaml:"threshold_ratio,omitempty"`
	TargetRatio    float64 `yaml:"target_ratio,omitempty"`
	Model          string  `yaml:"model,omitempty"`

	enabledSet bool
}

func (c *AutoCompactConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawAutoCompactConfig struct {
		Enabled             *bool   `yaml:"enabled"`
		ThresholdRatio      float64 `yaml:"threshold_ratio"`
		ThresholdRatioCamel float64 `yaml:"thresholdRatio"`
		TargetRatio         float64 `yaml:"target_ratio"`
		TargetRatioCamel    float64 `yaml:"targetRatio"`
		Model               string  `yaml:"model"`
	}
	var raw rawAutoCompactConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.Enabled != nil {
		c.Enabled = *raw.Enabled
		c.enabledSet = true
	}
	c.ThresholdRatio = firstNonZeroFloat(raw.ThresholdRatio, raw.ThresholdRatioCamel)
	c.TargetRatio = firstNonZeroFloat(raw.TargetRatio, raw.TargetRatioCamel)
	c.Model = strings.TrimSpace(raw.Model)
	return nil
}

// RegistryConfig configures the plugin registry connection.
type RegistryConfig struct {
	URL   string `yaml:"url" json:"url"`
	Token string `yaml:"token" json:"token,omitempty"`
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	type rawConfig struct {
		DefaultProvider string                    `yaml:"default_provider"`
		DefaultModel    string                    `yaml:"default_model"`
		Providers       yaml.Node                 `yaml:"providers"`
		UI              UIConfig                  `yaml:"ui"`
		Debug           bool                      `yaml:"debug"`
		Registry        RegistryConfig            `yaml:"registry"`
		Plugins         map[string]map[string]any `yaml:"plugins"`
		DisabledSkills  []string                  `yaml:"disabled_skills"`
		SkillPaths      []string                  `yaml:"skill_paths"`
		Metrics         MetricsConfig             `yaml:"metrics"`
		AutoCompact     AutoCompactConfig         `yaml:"auto_compact"`
	}
	var raw rawConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	c.DefaultProvider = raw.DefaultProvider
	c.DefaultModel = raw.DefaultModel
	c.UI = raw.UI
	c.Debug = raw.Debug
	c.Registry = raw.Registry
	c.Plugins = raw.Plugins
	c.DisabledSkills = raw.DisabledSkills
	c.SkillPaths = raw.SkillPaths
	c.Metrics = raw.Metrics
	c.AutoCompact = raw.AutoCompact
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
	ShowReasoning             bool `yaml:"show_reasoning" json:"show_reasoning"`
	ToolCallsDefaultCollapsed bool `yaml:"tool_calls_default_collapsed" json:"tool_calls_default_collapsed"`

	showReasoningSet             bool
	toolCallsDefaultCollapsedSet bool
}

func (c *UIConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawUIConfig struct {
		ShowReasoning             *bool `yaml:"show_reasoning"`
		ToolCallsDefaultCollapsed *bool `yaml:"tool_calls_default_collapsed"`
	}
	var raw rawUIConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.ShowReasoning != nil {
		c.ShowReasoning = *raw.ShowReasoning
		c.showReasoningSet = true
	}
	if raw.ToolCallsDefaultCollapsed != nil {
		c.ToolCallsDefaultCollapsed = *raw.ToolCallsDefaultCollapsed
		c.toolCallsDefaultCollapsedSet = true
	}
	return nil
}

// ProviderConfig describes an OpenAI-compatible chat provider.
type ProviderConfig struct {
	Name    string            `yaml:"name" json:"name"`
	Type    string            `yaml:"type,omitempty" json:"type,omitempty"`
	API     string            `yaml:"api,omitempty" json:"api,omitempty"`
	BaseURL string            `yaml:"base_url" json:"base_url"`
	Auth    AuthConfig        `yaml:"auth" json:"auth"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Models  []ModelConfig     `yaml:"models,omitempty" json:"models,omitempty"`
}

func (p *ProviderConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawProviderConfig struct {
		Name         string            `yaml:"name"`
		Type         string            `yaml:"type"`
		API          string            `yaml:"api"`
		BaseURL      string            `yaml:"base_url"`
		BaseURLCamel string            `yaml:"baseUrl"`
		Auth         AuthConfig        `yaml:"auth"`
		Headers      map[string]string `yaml:"headers"`
		APIKey       string            `yaml:"api_key"`
		APIKeyCamel  string            `yaml:"apiKey"`
		APIKeyEnv    string            `yaml:"api_key_env"`
		Models       modelConfigs      `yaml:"models"`
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
	p.Headers = cleanStringMap(raw.Headers)
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
	Type            string `yaml:"type" json:"type"`
	APIKeyEnv       string `yaml:"api_key_env" json:"api_key_env,omitempty"`
	APIKey          string `yaml:"api_key" json:"api_key,omitempty"`
	AuthorizeURL    string `yaml:"authorize_url" json:"authorize_url,omitempty"`
	TokenURL        string `yaml:"token_url" json:"token_url,omitempty"`
	ClientID        string `yaml:"client_id" json:"client_id,omitempty"`
	IDP             string `yaml:"idp" json:"idp,omitempty"`
	TokenAuthMethod string `yaml:"token_auth_method" json:"token_auth_method,omitempty"`
}

func (a *AuthConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawAuthConfig struct {
		Type                 string `yaml:"type"`
		APIKeyEnv            string `yaml:"api_key_env"`
		APIKeyEnvCamel       string `yaml:"apiKeyEnv"`
		APIKey               string `yaml:"api_key"`
		APIKeyCamel          string `yaml:"apiKey"`
		AuthorizeURL         string `yaml:"authorize_url"`
		AuthorizeURLCamel    string `yaml:"authorizeUrl"`
		TokenURL             string `yaml:"token_url"`
		TokenURLCamel        string `yaml:"tokenUrl"`
		ClientID             string `yaml:"client_id"`
		ClientIDCamel        string `yaml:"clientId"`
		IDP                  string `yaml:"idp"`
		TokenAuthMethod      string `yaml:"token_auth_method"`
		TokenAuthMethodCamel string `yaml:"tokenAuthMethod"`
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
	a.TokenAuthMethod = firstNonEmpty(raw.TokenAuthMethod, raw.TokenAuthMethodCamel)
	return nil
}

// ModelConfig stores optional configured model metadata for a provider.
type ModelConfig struct {
	ID               string   `yaml:"id" json:"id"`
	Name             string   `yaml:"name,omitempty" json:"name,omitempty"`
	URL              string   `yaml:"url,omitempty" json:"url,omitempty"`
	ContextWindow    int      `yaml:"context_window,omitempty" json:"context_window,omitempty"`
	DefaultMaxTokens int      `yaml:"default_max_tokens,omitempty" json:"default_max_tokens,omitempty"`
	MaxTokens        int      `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Input            []string `yaml:"input,omitempty" json:"input,omitempty"`
	Reasoning        bool     `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
	ReasoningEffort  string   `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
	// ReasoningEfforts are the effort levels the model accepts (from models.dev
	// reasoning_options), e.g. [low medium high]. Empty when the model exposes
	// no selectable effort (reasoning is fixed) or isn't a reasoning model.
	ReasoningEfforts []string `yaml:"reasoning_efforts,omitempty" json:"reasoning_efforts,omitempty"`
	// ReasoningBudgetMax is the ceiling (in tokens) used to compute
	// budget-based effort levels for reasoning models that don't
	// advertise explicit effort-type options. Zero means not applicable.
	ReasoningBudgetMax int            `json:"reasoning_budget_max,omitempty"`
	Thinking           ThinkingConfig `yaml:"thinking,omitempty" json:"thinking"`
	Cost               CostConfig     `yaml:"cost,omitempty" json:"cost"`
	Compat             CompatConfig   `yaml:"compat,omitempty" json:"compat"`
}

func (m *ModelConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawModelConfig struct {
		ID                                               string            `yaml:"id"`
		Name                                             string            `yaml:"name"`
		URL                                              string            `yaml:"url"`
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
		ReasoningEffort                                  string            `yaml:"reasoning_effort"`
		ReasoningEffortCamel                             string            `yaml:"reasoningEffort"`
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
	m.URL = raw.URL
	m.ContextWindow = firstNonZero(raw.ContextWindow, raw.ContextWindowCamel)
	m.DefaultMaxTokens = firstNonZero(raw.DefaultMaxTokens, raw.DefaultMaxTokensCamel)
	m.MaxTokens = firstNonZero(raw.MaxTokens, raw.MaxTokensCamel)
	m.Input = append([]string(nil), raw.Input...)
	m.Reasoning = raw.Reasoning || raw.CanReason || raw.CanReasonCamel
	m.Thinking = raw.Thinking
	m.ReasoningEffort = firstNonEmpty(raw.ReasoningEffort, raw.ReasoningEffortCamel)
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

	// TokenAuthMethodPost sends client_id in the form body (standard OAuth 2.0, default).
	TokenAuthMethodPost = "post"
	// TokenAuthMethodBasic sends client_id and client_secret via HTTP Basic auth.
	TokenAuthMethodBasic = "basic"
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

// SessionsDir returns the directory where session JSONL exports are stored.
func SessionsDir() string {
	return filepath.Join(Dir(), "sessions")
}

// SessionsDBPath returns the path to the SQLite session store.
func SessionsDBPath() string {
	return filepath.Join(Dir(), "sessions.db")
}

// MetricsDir returns the default directory for metrics.jsonl export — used
// by syncConfigSchema to backfill MetricsConfig.Dir when it's missing/empty
// in an existing config file.
func MetricsDir() string {
	return filepath.Join(Dir(), "metrics")
}

// ScheduleIntervalFromEnv returns the schedule interval from TAU_SCHEDULE_INTERVAL.
// Returns 0 if unset or invalid.
func ScheduleIntervalFromEnv() time.Duration {
	raw := os.Getenv("TAU_SCHEDULE_INTERVAL")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return d
}

func configDir() string {
	if dir := os.Getenv("TAU_CONFIG_DIR"); dir != "" {
		return dir
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "tau")
}

// LoadConfig loads and merges global and project-local configuration.
func LoadConfig() (Config, error) {
	cwd, _ := os.Getwd()
	return LoadConfigFrom(cwd)
}

// LoadConfigFrom loads and merges global config and .tau.yaml from cwd,
// requiring at least one configured provider.
func LoadConfigFrom(cwd string) (Config, error) {
	return loadConfigFrom(cwd, true)
}

// LoadConfigAllowEmpty loads and merges configuration without requiring any
// providers. It is used by the provider-management layer, which supplements
// hand-written config with auto-detected and OAuth providers; an empty config
// is a valid starting point there rather than a fatal error.
func LoadConfigAllowEmpty() (Config, error) {
	cwd, _ := os.Getwd()
	return loadConfigFrom(cwd, false)
}

func loadConfigFrom(cwd string, requireProviders bool) (Config, error) {
	globalPath := GlobalPath()
	localPath := LocalPath(cwd)

	globalCfg, globalFound, err := readConfigFile(globalPath)
	if err != nil {
		return Config{}, err
	}
	if globalFound {
		if err := syncConfigSchema(globalPath); err != nil {
			slog.Warn("config: syncing schema defaults into config file", "path", globalPath, "err", err)
		}
	}
	localCfg, localFound, err := readConfigFile(localPath)
	if err != nil {
		return Config{}, err
	}
	cfg := mergeConfigs(globalCfg, localCfg)
	if !cfg.UI.showReasoningSet {
		cfg.UI.ShowReasoning = true
	}
	if len(cfg.Providers) == 0 && requireProviders {
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

// schemaBlock is a top-level, struct-typed Config field: a named block of
// related settings (e.g. "metrics", "registry", "ui") as opposed to a
// scalar or a user-authored collection like providers/plugins.
type schemaBlock struct {
	key  string
	zero any
}

// schemaBlocks reflects over Config's fields to discover every top-level
// struct-typed block the current binary knows about, along with each
// block's zero value encoded as a plain YAML-decodable value. Discovering
// blocks via reflection (rather than a hand-maintained list) means a newly
// added struct field on Config is picked up automatically the next time
// this runs, so the schema-sync stays correct without further changes here.
func schemaBlocks() []schemaBlock {
	t := reflect.TypeFor[Config]()
	blocks := make([]schemaBlock, 0, t.NumField())
	for field := range t.Fields() {
		if field.Type.Kind() != reflect.Struct {
			continue
		}
		key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if key == "" || key == "-" {
			continue
		}
		zeroVal := reflect.New(field.Type).Elem().Interface()
		encoded, err := yaml.Marshal(zeroVal)
		if err != nil {
			continue
		}
		var decoded any
		if err := yaml.Unmarshal(encoded, &decoded); err != nil {
			continue
		}
		blocks = append(blocks, schemaBlock{key: key, zero: decoded})
	}
	return blocks
}

// syncConfigSchema ensures the config file at path contains every top-level
// struct-typed block known to the current schema (e.g. "metrics",
// "registry", "ui"), adding any that are entirely missing with their
// zero-value defaults so the file's schema stays discoverable across Tau
// upgrades. It never touches a key that's already present, even if it's set
// to a zero value, and it never creates a file that doesn't exist or touches
// user-authored collections like providers/plugins. The one exception is
// metrics.dir (see backfillMetricsDir): an empty/missing dir there almost
// always means the block was auto-generated by an earlier run of this same
// function, at a time when this field had no non-zero default — not a user
// deliberately disabling metrics export — so it gets backfilled to
// MetricsDir(). It is a no-op if nothing is missing.
func syncConfigSchema(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}

	raw := make(map[string]any)
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}

	changed := false
	for _, block := range schemaBlocks() {
		if _, ok := raw[block.key]; ok {
			continue
		}
		raw[block.key] = block.zero
		changed = true
	}
	if backfillMetricsDir(raw) {
		changed = true
	}
	if !changed {
		return nil
	}

	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// backfillMetricsDir sets raw["metrics"]["dir"] to MetricsDir() when the
// metrics block is present but its dir is missing or empty, so file export
// is on by default for existing configs (not just newly-generated ones —
// see syncConfigSchema's doc comment). Reports whether it changed anything.
func backfillMetricsDir(raw map[string]any) bool {
	metrics, ok := raw["metrics"].(map[string]any)
	if !ok {
		return false
	}
	if dir, ok := metrics["dir"].(string); ok && dir != "" {
		return false
	}
	metrics["dir"] = MetricsDir()
	return true
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

func cleanStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for k, v := range value {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonNilAnyMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// mergeMetricsConfigs merges local metrics config on top of global.
func mergeMetricsConfigs(globalCfg, localCfg MetricsConfig) MetricsConfig {
	merged := globalCfg
	if strings.TrimSpace(localCfg.Dir) != "" {
		merged.Dir = localCfg.Dir
	}
	if localCfg.Session {
		merged.Session = true
	}
	if localCfg.TUI {
		merged.TUI = true
	}
	return merged
}

// mergeAutoCompactConfigs merges project-local auto-compaction config over
// global config, preserving false as an explicit override for enabled.
func mergeAutoCompactConfigs(globalCfg, localCfg AutoCompactConfig) AutoCompactConfig {
	merged := globalCfg
	if localCfg.enabledSet {
		merged.Enabled = localCfg.Enabled
		merged.enabledSet = true
	}
	if localCfg.ThresholdRatio > 0 {
		merged.ThresholdRatio = localCfg.ThresholdRatio
	}
	if localCfg.TargetRatio > 0 {
		merged.TargetRatio = localCfg.TargetRatio
	}
	if strings.TrimSpace(localCfg.Model) != "" {
		merged.Model = localCfg.Model
	}
	return merged
}

// mergeStringSlices combines two slices, deduplicating entries. Local
// entries are appended after global entries; duplicates are dropped so the
// local config can add to the global list without repeating items.
func mergeStringSlices(global, local []string) []string {
	if len(global) == 0 {
		if len(local) == 0 {
			return nil
		}
		out := make([]string, len(local))
		copy(out, local)
		return out
	}
	if len(local) == 0 {
		out := make([]string, len(global))
		copy(out, global)
		return out
	}
	seen := make(map[string]struct{}, len(global)+len(local))
	out := make([]string, 0, len(global)+len(local))
	for _, v := range global {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		seen[strings.ToLower(trimmed)] = struct{}{}
		out = append(out, v)
	}
	for _, v := range local {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[strings.ToLower(trimmed)]; exists {
			continue
		}
		out = append(out, v)
	}
	return out
}

func mergeConfigs(globalCfg, localCfg Config) Config {
	merged := globalCfg
	if strings.TrimSpace(localCfg.DefaultProvider) != "" {
		merged.DefaultProvider = localCfg.DefaultProvider
	}
	if strings.TrimSpace(localCfg.DefaultModel) != "" {
		merged.DefaultModel = localCfg.DefaultModel
	}
	merged.Debug = globalCfg.Debug || localCfg.Debug
	merged.Metrics = mergeMetricsConfigs(merged.Metrics, localCfg.Metrics)
	merged.AutoCompact = mergeAutoCompactConfigs(merged.AutoCompact, localCfg.AutoCompact)

	// Local registry config overrides global (URL and token).
	if strings.TrimSpace(localCfg.Registry.URL) != "" {
		merged.Registry.URL = localCfg.Registry.URL
	}
	if strings.TrimSpace(localCfg.Registry.Token) != "" {
		merged.Registry.Token = localCfg.Registry.Token
	}

	// Merge disabled skills: local appends to global (project-local can add
	// more disabled skills but cannot re-enable globally disabled ones).
	merged.DisabledSkills = mergeStringSlices(globalCfg.DisabledSkills, localCfg.DisabledSkills)
	// Merge skill paths: local appends to global.
	merged.SkillPaths = mergeStringSlices(globalCfg.SkillPaths, localCfg.SkillPaths)

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
	merged.UI = mergeUIConfigs(merged.UI, localCfg.UI)
	return merged
}

func mergeUIConfigs(globalCfg, localCfg UIConfig) UIConfig {
	merged := globalCfg
	if localCfg.showReasoningSet {
		merged.ShowReasoning = localCfg.ShowReasoning
		merged.showReasoningSet = true
	}
	if localCfg.toolCallsDefaultCollapsedSet {
		merged.ToolCallsDefaultCollapsed = localCfg.ToolCallsDefaultCollapsed
		merged.toolCallsDefaultCollapsedSet = true
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

// SaveDefaultProviderAndModel writes default_provider and/or default_model
// to the global Tau config file (config dir + config.yaml) so the selection
// persists across restarts. Other fields are preserved. The file is created
// if it doesn't already exist. An empty provider or model string is silently
// skipped.
func SaveDefaultProviderAndModel(cwd, provider, model string) error {
	path := GlobalPath()
	_ = cwd // kept for API compatibility

	data := make(map[string]any)

	if b, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(b, &data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read local config: %w", err)
	}

	if strings.TrimSpace(provider) != "" {
		data["default_provider"] = provider
	}
	if strings.TrimSpace(model) != "" {
		data["default_model"] = model
	}

	out, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal local config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write local config: %w", err)
	}
	return nil
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
