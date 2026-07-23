package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestConfigStructParityRoundTrip is the structural parity guard for Config.
// It ensures every exported YAML-tagged field on Config is represented in
// rawConfig and survives a YAML round-trip with the non-zero value set here.
//
// When adding a new YAML field to Config:
//  1. Add the field to Config (the real struct).
//  2. Add the corresponding field to rawConfig inside UnmarshalYAML.
//  3. Add a non-zero value for it in the YAML literal below AND an assertion.
//
// Skipping step 2 or 3 will make this test fail - fields that aren't in
// rawConfig decode to zero, and the assertion below will catch it.
func TestConfigStructParityRoundTrip(t *testing.T) {
	// AUTOMATIC PARITY CHECK: every exported YAML field on Config must be
	// verified non-zero after unmarshal. This catches fields added to the
	// struct but not to the YAML literal or rawConfig.
	configType := reflect.TypeOf(Config{})
	fieldNames := collectExportedYAMLFieldNames(configType)

	var cfg Config
	err := yaml.Unmarshal([]byte(`
default_provider: test-provider
default_model: test-model
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
ui:
  show_reasoning: true
  tool_calls_default_collapsed: true
debug: true
registry:
  url: https://registry.example
  token: registry-token
plugins:
  my-plugin:
    key: value
disabled_skills:
  - blocked-skill
skill_paths:
  - /custom/skills
model_modes:
  fast:
    provider: fast-provider
    model: fast-model
agents:
  default_max_depth: 3
  depth_ceiling: 6
  default_max_turns: 20
  default_timeout: 15m
  cancel_grace: 10s
  kill_grace: 10s
  max_active_children: 2
  max_total_children: 8
  max_queued_spawns: 4
  orphan_stale_age: 12h
metrics:
  dir: /custom/metrics
  session: true
  tui: true
auto_compact:
  enabled: true
  threshold_ratio: 0.8
  target_ratio: 0.25
  model: compact-model
`), &cfg)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Explicit field assertions - one per YAML-tagged Config field.
	// Each of these must be set explicitly in the YAML literal above.
	checkStr(t, "default_provider", cfg.DefaultProvider, "test-provider")
	checkStr(t, "default_model", cfg.DefaultModel, "test-model")
	checkBool(t, "debug", cfg.Debug, true)
	checkTrue(t, "ui.show_reasoning", cfg.UI.ShowReasoning)
	checkTrue(t, "ui.tool_calls_default_collapsed", cfg.UI.ToolCallsDefaultCollapsed)
	checkStr(t, "registry.url", cfg.Registry.URL, "https://registry.example")
	checkStr(t, "registry.token", cfg.Registry.Token, "registry-token")
	checkTrue(t, "metrics.session", cfg.Metrics.Session)
	checkTrue(t, "metrics.tui", cfg.Metrics.TUI)
	checkStr(t, "metrics.dir", cfg.Metrics.Dir, "/custom/metrics")
	checkTrue(t, "auto_compact.enabled", cfg.AutoCompact.Enabled)
	checkFloatEq(t, "auto_compact.threshold_ratio", cfg.AutoCompact.ThresholdRatio, 0.8)
	checkFloatEq(t, "auto_compact.target_ratio", cfg.AutoCompact.TargetRatio, 0.25)
	checkStr(t, "auto_compact.model", cfg.AutoCompact.Model, "compact-model")

	// Providers
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers: got %d, want 1", len(cfg.Providers))
	}
	checkStr(t, "providers[0].name", cfg.Providers[0].Name, "acme")
	checkStr(t, "providers[0].base_url", cfg.Providers[0].BaseURL, "https://acme.example")

	// Plugins
	if cfg.Plugins == nil || cfg.Plugins["my-plugin"] == nil {
		t.Fatal("plugins: my-plugin not found")
	}
	if v, ok := cfg.Plugins["my-plugin"]["key"].(string); !ok || v != "value" {
		t.Fatalf("plugins.my-plugin.key = %v, want 'value'", cfg.Plugins["my-plugin"]["key"])
	}

	// DisabledSkills & SkillPaths
	if len(cfg.DisabledSkills) != 1 || cfg.DisabledSkills[0] != "blocked-skill" {
		t.Fatalf("disabled_skills = %v, want [blocked-skill]", cfg.DisabledSkills)
	}
	if len(cfg.SkillPaths) != 1 || cfg.SkillPaths[0] != "/custom/skills" {
		t.Fatalf("skill_paths = %v, want [/custom/skills]", cfg.SkillPaths)
	}

	// ModelModes
	if len(cfg.ModelModes) != 1 {
		t.Fatalf("model_modes: got %d entries, want 1", len(cfg.ModelModes))
	}
	mm, ok := cfg.ModelModes["fast"]
	if !ok {
		t.Fatal("model_modes: missing 'fast' key")
	}
	checkStr(t, "model_modes.fast.provider", mm.Provider, "fast-provider")
	checkStr(t, "model_modes.fast.model", mm.Model, "fast-model")

	// AgentsConfig
	a := cfg.Agents
	checkIntEq(t, "agents.default_max_depth", a.DefaultMaxDepth, 3)
	checkIntEq(t, "agents.depth_ceiling", a.DepthCeiling, 6)
	checkIntEq(t, "agents.default_max_turns", a.DefaultMaxTurns, 20)
	checkDurEq(t, "agents.default_timeout", a.DefaultTimeout, 15*time.Minute)
	checkDurEq(t, "agents.cancel_grace", a.CancelGrace, 10*time.Second)
	checkDurEq(t, "agents.kill_grace", a.KillGrace, 10*time.Second)
	checkIntEq(t, "agents.max_active_children", a.MaxActiveChildren, 2)
	checkIntEq(t, "agents.max_total_children", a.MaxTotalChildren, 8)
	checkIntEq(t, "agents.max_queued_spawns", a.MaxQueuedSpawns, 4)
	checkDurEq(t, "agents.orphan_stale_age", a.OrphanStaleAge, 12*time.Hour)

	// Automated parity: verify every exported YAML field was checked.
	checked := map[string]bool{
		"agents": true, "auto_compact": true, "debug": true, "default_model": true,
		"default_provider": true, "disabled_skills": true, "metrics": true,
		"model_modes": true, "plugins": true, "providers": true, "registry": true,
		"skill_paths": true, "ui": true,
	}
	for _, name := range fieldNames {
		if !checked[name] {
			t.Errorf("exported YAML field %q on Config is not verified in this test - add it to the YAML literal, rawConfig, and assertions above", name)
		}
	}
}

// TestAgentsConfigParityRoundTrip tests AgentsConfig decoder parity.
func TestAgentsConfigParityRoundTrip(t *testing.T) {
	var a AgentsConfig
	err := yaml.Unmarshal([]byte(`
default_max_depth: 5
depth_ceiling: 8
default_max_turns: 40
default_timeout: 20m
cancel_grace: 15s
kill_grace: 15s
max_active_children: 6
max_total_children: 24
max_queued_spawns: 12
orphan_stale_age: 48h
`), &a)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkIntEq(t, "default_max_depth", a.DefaultMaxDepth, 5)
	checkIntEq(t, "depth_ceiling", a.DepthCeiling, 8)
	checkIntEq(t, "default_max_turns", a.DefaultMaxTurns, 40)
	checkDurEq(t, "default_timeout", a.DefaultTimeout, 20*time.Minute)
	checkDurEq(t, "cancel_grace", a.CancelGrace, 15*time.Second)
	checkDurEq(t, "kill_grace", a.KillGrace, 15*time.Second)
	checkIntEq(t, "max_active_children", a.MaxActiveChildren, 6)
	checkIntEq(t, "max_total_children", a.MaxTotalChildren, 24)
	checkIntEq(t, "max_queued_spawns", a.MaxQueuedSpawns, 12)
	checkDurEq(t, "orphan_stale_age", a.OrphanStaleAge, 48*time.Hour)

	// Ensure all exported YAML fields on AgentsConfig are covered.
	agentFields := collectExportedYAMLFieldNames(reflect.TypeOf(AgentsConfig{}))
	checked := map[string]bool{
		"cancel_grace": true, "default_max_depth": true, "default_max_turns": true,
		"default_timeout": true, "depth_ceiling": true, "kill_grace": true,
		"max_active_children": true, "max_queued_spawns": true, "max_total_children": true,
		"orphan_stale_age": true,
	}
	for _, name := range agentFields {
		if !checked[name] {
			t.Errorf("exported YAML field %q on AgentsConfig is not verified - add it to the YAML literal and assertions above", name)
		}
	}
}

// TestAgentsConfigCamelCaseAliases verifies camelCase aliases work.
func TestAgentsConfigCamelCaseAliases(t *testing.T) {
	var a AgentsConfig
	err := yaml.Unmarshal([]byte(`
defaultMaxDepth: 5
depthCeiling: 8
defaultMaxTurns: 40
defaultTimeout: 20m
cancelGrace: 15s
killGrace: 15s
maxActiveChildren: 6
maxTotalChildren: 24
maxQueuedSpawns: 12
orphanStaleAge: 48h
`), &a)
	if err != nil {
		t.Fatalf("Unmarshal() with camelCase error = %v", err)
	}
	checkIntEq(t, "default_max_depth (via defaultMaxDepth)", a.DefaultMaxDepth, 5)
	checkDurEq(t, "default_timeout (via defaultTimeout)", a.DefaultTimeout, 20*time.Minute)
}

// TestAgentsConfigDurationErrors verifies malformed durations produce errors.
func TestAgentsConfigDurationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"bad default_timeout", "default_timeout: wibble"},
		{"bad cancel_grace", "cancel_grace: wibble"},
		{"bad kill_grace", "kill_grace: wibble"},
		{"bad orphan_stale_age", "orphan_stale_age: wibble"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a AgentsConfig
			err := yaml.Unmarshal([]byte(tt.yaml), &a)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.name)
			}
		})
	}
}

// TestUIConfigParityRoundTrip tests UIConfig decoder parity.
func TestUIConfigParityRoundTrip(t *testing.T) {
	var ui UIConfig
	err := yaml.Unmarshal([]byte(`
show_reasoning: true
tool_calls_default_collapsed: true
`), &ui)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkTrue(t, "show_reasoning", ui.ShowReasoning)
	checkTrue(t, "tool_calls_default_collapsed", ui.ToolCallsDefaultCollapsed)

	// Verify explicit false isn't treated as "unset" (distinct from zero-value default).
	var ui2 UIConfig
	err = yaml.Unmarshal([]byte(`
show_reasoning: false
`), &ui2)
	if err != nil {
		t.Fatalf("Unmarshal() with false error = %v", err)
	}
	if ui2.ShowReasoning {
		t.Error("show_reasoning should be false after explicit false in YAML")
	}
	if !ui2.showReasoningSet {
		t.Error("showReasoningSet should be true after explicit false")
	}
}

// TestAutoCompactConfigParityRoundTrip tests AutoCompactConfig decoder parity.
func TestAutoCompactConfigParityRoundTrip(t *testing.T) {
	var ac AutoCompactConfig
	err := yaml.Unmarshal([]byte(`
enabled: true
threshold_ratio: 0.8
targetRatio: 0.25
model: compact-model
`), &ac)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkTrue(t, "enabled", ac.Enabled)
	checkFloatEq(t, "threshold_ratio", ac.ThresholdRatio, 0.8)
	checkFloatEq(t, "target_ratio", ac.TargetRatio, 0.25)
	checkStr(t, "model", ac.Model, "compact-model")
	if !ac.enabledSet {
		t.Error("enabledSet should be true after explicit YAML enabled: true")
	}
}

// TestProviderConfigParityRoundTrip tests ProviderConfig decoder parity.
func TestProviderConfigParityRoundTrip(t *testing.T) {
	var p ProviderConfig
	err := yaml.Unmarshal([]byte(`
name: test-provider
type: custom
api: openai
base_url: https://test.example
auth:
  type: api_key
  api_key_env: TEST_KEY
headers:
  X-Custom: value
models:
  - id: model-1
`), &p)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkStr(t, "name", p.Name, "test-provider")
	checkStr(t, "type", p.Type, "custom")
	checkStr(t, "api", p.API, "openai")
	checkStr(t, "base_url", p.BaseURL, "https://test.example")
	checkStr(t, "auth.type", p.Auth.Type, "api_key")
	checkStr(t, "auth.api_key_env", p.Auth.APIKeyEnv, "TEST_KEY")
	if p.Headers == nil || p.Headers["X-Custom"] != "value" {
		t.Fatalf("headers = %v, want {X-Custom: value}", p.Headers)
	}
	if len(p.Models) != 1 || p.Models[0].ID != "model-1" {
		t.Fatalf("models = %v, want [{id: model-1}]", p.Models)
	}

	// CamelCase alias for base_url.
	var p2 ProviderConfig
	err = yaml.Unmarshal([]byte("name: p2\nbaseUrl: https://camel.example\nauth:\n  type: none\n"), &p2)
	if err != nil {
		t.Fatalf("Unmarshal() with baseUrl error = %v", err)
	}
	checkStr(t, "base_url (via baseUrl)", p2.BaseURL, "https://camel.example")
}

// TestAuthConfigParityRoundTrip tests AuthConfig decoder parity.
func TestAuthConfigParityRoundTrip(t *testing.T) {
	var a AuthConfig
	err := yaml.Unmarshal([]byte(`
type: oauth
api_key_env: MY_KEY
authorize_url: https://auth.example
token_url: https://token.example
client_id: my-client
idp: github
token_auth_method: client_secret_post
`), &a)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkStr(t, "type", a.Type, "oauth")
	checkStr(t, "api_key_env", a.APIKeyEnv, "MY_KEY")
	checkStr(t, "authorize_url", a.AuthorizeURL, "https://auth.example")
	checkStr(t, "token_url", a.TokenURL, "https://token.example")
	checkStr(t, "client_id", a.ClientID, "my-client")
	checkStr(t, "idp", a.IDP, "github")
	checkStr(t, "token_auth_method", a.TokenAuthMethod, "client_secret_post")
}

// TestModelConfigParityRoundTrip tests ModelConfig decoder parity.
func TestModelConfigParityRoundTrip(t *testing.T) {
	var m ModelConfig
	err := yaml.Unmarshal([]byte(`
id: gpt-5
name: GPT-5
url: https://model.example
context_window: 131072
default_max_tokens: 4096
max_tokens: 16384
input:
  - text
reasoning: true
reasoning_effort: high
thinking:
  min_level: low
  max_level: high
  mode: auto
cost:
  input: 2.5
  output: 10.0
  cache_read: 1.25
  cache_write: 5.0
compat:
  supports_tool_choice: true
  supports_developer_role: true
  requires_assistant_content_for_tool_calls: true
`), &m)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkStr(t, "id", m.ID, "gpt-5")
	checkStr(t, "name", m.Name, "GPT-5")
	checkStr(t, "url", m.URL, "https://model.example")
	checkIntEq(t, "context_window", m.ContextWindow, 131072)
	checkIntEq(t, "default_max_tokens", m.DefaultMaxTokens, 4096)
	checkIntEq(t, "max_tokens", m.MaxTokens, 16384)
	if len(m.Input) != 1 || m.Input[0] != "text" {
		t.Fatalf("input = %v, want [text]", m.Input)
	}
	checkTrue(t, "reasoning", m.Reasoning)
	checkStr(t, "reasoning_effort", m.ReasoningEffort, "high")
	checkStr(t, "thinking.min_level", m.Thinking.MinLevel, "low")
	checkStr(t, "thinking.max_level", m.Thinking.MaxLevel, "high")
	checkStr(t, "thinking.mode", m.Thinking.Mode, "auto")
	checkFloatEq(t, "cost.input", m.Cost.Input, 2.5)
	checkFloatEq(t, "cost.output", m.Cost.Output, 10.0)
	checkFloatEq(t, "cost.cache_read", m.Cost.CacheRead, 1.25)
	checkFloatEq(t, "cost.cache_write", m.Cost.CacheWrite, 5.0)
	if m.Compat.SupportsToolChoice == nil || !*m.Compat.SupportsToolChoice {
		t.Error("compat.supports_tool_choice should be true")
	}
	if m.Compat.SupportsDeveloperRole == nil || !*m.Compat.SupportsDeveloperRole {
		t.Error("compat.supports_developer_role should be true")
	}
	checkTrue(t, "compat.requires_assistant_content_for_tool_calls", m.Compat.RequiresAssistantContentForToolCalls)
}

// TestThinkingConfigParityRoundTrip tests ThinkingConfig decoder parity.
func TestThinkingConfigParityRoundTrip(t *testing.T) {
	var tc ThinkingConfig
	err := yaml.Unmarshal([]byte(`
min_level: low
max_level: high
mode: auto
`), &tc)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkStr(t, "min_level", tc.MinLevel, "low")
	checkStr(t, "max_level", tc.MaxLevel, "high")
	checkStr(t, "mode", tc.Mode, "auto")

	// CamelCase aliases.
	var tc2 ThinkingConfig
	err = yaml.Unmarshal([]byte("minLevel: med\nmaxLevel: high\n"), &tc2)
	if err != nil {
		t.Fatalf("Unmarshal() with camelCase error = %v", err)
	}
	checkStr(t, "min_level (via minLevel)", tc2.MinLevel, "med")
	checkStr(t, "max_level (via maxLevel)", tc2.MaxLevel, "high")
}

// TestCostConfigParityRoundTrip tests CostConfig decoder parity.
func TestCostConfigParityRoundTrip(t *testing.T) {
	var c CostConfig
	err := yaml.Unmarshal([]byte(`
input: 2.5
output: 10.0
cache_read: 1.25
cache_write: 5.0
`), &c)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkFloatEq(t, "input", c.Input, 2.5)
	checkFloatEq(t, "output", c.Output, 10.0)
	checkFloatEq(t, "cache_read", c.CacheRead, 1.25)
	checkFloatEq(t, "cache_write", c.CacheWrite, 5.0)

	// CamelCase aliases for cache fields.
	var c2 CostConfig
	err = yaml.Unmarshal([]byte("cacheRead: 0.5\ncacheWrite: 2.0\n"), &c2)
	if err != nil {
		t.Fatalf("Unmarshal() with camelCase error = %v", err)
	}
	checkFloatEq(t, "cache_read (via cacheRead)", c2.CacheRead, 0.5)
	checkFloatEq(t, "cache_write (via cacheWrite)", c2.CacheWrite, 2.0)
}

// TestCompatConfigParityRoundTrip tests CompatConfig decoder parity.
func TestCompatConfigParityRoundTrip(t *testing.T) {
	var c CompatConfig
	err := yaml.Unmarshal([]byte(`
max_tokens_field: max_tokens
supports_developer_role: true
supports_reasoning_effort: true
supports_tool_choice: true
requires_assistant_content_for_tool_calls: true
requires_reasoning_content_for_tool_calls: true
requires_reasoning_content_on_assistant_messages: true
thinking_format: anthropic
reasoning_effort_map:
  low: 1000
  high: 8000
extra_body:
  custom_param: value
`), &c)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	checkStr(t, "max_tokens_field", c.MaxTokensField, "max_tokens")
	if c.SupportsDeveloperRole == nil || !*c.SupportsDeveloperRole {
		t.Error("supports_developer_role should be true")
	}
	checkTrue(t, "supports_reasoning_effort", c.SupportsReasoningEffort)
	if c.SupportsToolChoice == nil || !*c.SupportsToolChoice {
		t.Error("supports_tool_choice should be true")
	}
	checkTrue(t, "requires_assistant_content_for_tool_calls", c.RequiresAssistantContentForToolCalls)
	checkTrue(t, "requires_reasoning_content_for_tool_calls", c.RequiresReasoningContentForToolCalls)
	checkTrue(t, "requires_reasoning_content_on_assistant_messages", c.RequiresReasoningContentOnAssistantMessages)
	checkStr(t, "thinking_format", c.ThinkingFormat, "anthropic")
	if c.ReasoningEffortMap == nil || c.ReasoningEffortMap["low"] != "1000" {
		t.Fatalf("reasoning_effort_map = %v, want {low: 1000, high: 8000}", c.ReasoningEffortMap)
	}
	if c.ExtraBody == nil || c.ExtraBody["custom_param"] != "value" {
		t.Fatalf("extra_body = %v, want {custom_param: value}", c.ExtraBody)
	}
}

// --- helpers ---

// collectExportedYAMLFieldNames returns the yaml tag names (excluding
// ",omitempty" and similar suffixes) of all exported fields on the given
// type. Fields tagged yaml:"-" are skipped.
func collectExportedYAMLFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func checkStr(t *testing.T, label, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", label, got, want)
	}
}

func checkBool(t *testing.T, label string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func checkTrue(t *testing.T, label string, got bool) {
	t.Helper()
	if !got {
		t.Errorf("%s = false, want true", label)
	}
}

func checkIntEq(t *testing.T, label string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", label, got, want)
	}
}

func checkFloatEq(t *testing.T, label string, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func checkDurEq(t *testing.T, label string, got, want time.Duration) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}
