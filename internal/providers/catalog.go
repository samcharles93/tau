// Package providers manages tau's writable provider/auth layer: a built-in
// catalog of well-known OpenAI-compatible providers, a tau-owned state file
// recording which providers the user has enabled plus any OAuth credentials,
// and a resolver that merges hand-written config, that managed state, and the
// live environment into the effective set of providers tau should use.
//
// The user's hand-written config.yaml / .tau.yaml is treated as authoritative
// and is never written by this package — everything tau manages lives in a
// separate auth.yaml so comments and literal secrets are never clobbered.
package providers

import "strings"

// AuthKind classifies how a catalog provider authenticates.
type AuthKind string

const (
	// AuthAPIKey providers read a bearer token from an environment variable.
	AuthAPIKey AuthKind = "api_key"
	// AuthOAuth providers obtain a token through an interactive login flow and
	// persist the resulting credentials in the managed state file.
	AuthOAuth AuthKind = "oauth"
	// AuthNone providers need no credentials (e.g. a local Ollama server).
	AuthNone AuthKind = "none"
)

// CatalogEntry describes a well-known provider tau can enable without the user
// having to hand-write a config block. For API-key providers, EnvVars lists the
// conventional environment variables to probe (first one set wins).
type CatalogEntry struct {
	ID          string
	DisplayName string
	BaseURL     string
	EnvVars     []string
	Auth        AuthKind
	// CatalogID is the provider key under which models.dev publishes this
	// provider, when it differs from our ID (e.g. we call it "gemini", they
	// key it "google"). Empty means it matches ID. Used by the snapshot
	// generator to pull the right models.
	CatalogID string
	// LiveModels marks a provider whose model set is dynamic and machine- or
	// account-specific, so it is not baked into the embedded snapshot. tau
	// instead lists its models at runtime from the provider's /models endpoint
	// (e.g. a local Ollama server).
	LiveModels bool
	// Class names the ai-sdk runtime provider class to use when it is not the
	// default OpenAI-compatible one (e.g. "anthropic" for the native Messages
	// API). Empty means OpenAI-compatible.
	Class string
}

// SnapshotID returns the models.dev provider key for this entry, falling back to
// the tau ID when no override is set.
func (e CatalogEntry) SnapshotID() string {
	if strings.TrimSpace(e.CatalogID) != "" {
		return e.CatalogID
	}
	return e.ID
}

// catalog is the built-in provider list. It is intentionally small and
// conservative: each entry is an OpenAI-compatible chat endpoint (except the
// OAuth providers, whose surface is wired up by their login flow) with the
// environment variable names people most commonly export for that provider.
var catalog = []CatalogEntry{
	{ID: "openai", DisplayName: "OpenAI", BaseURL: "https://api.openai.com/v1", EnvVars: []string{"OPENAI_API_KEY"}, Auth: AuthAPIKey},
	{ID: "openrouter", DisplayName: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", EnvVars: []string{"OPENROUTER_API_KEY"}, Auth: AuthAPIKey},
	{ID: "deepseek", DisplayName: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", EnvVars: []string{"DEEPSEEK_API_KEY"}, Auth: AuthAPIKey},
	{ID: "together", DisplayName: "Together AI", BaseURL: "https://api.together.xyz/v1", EnvVars: []string{"TOGETHER_API_KEY", "TOGETHERAI_API_KEY"}, Auth: AuthAPIKey},
	{ID: "mistral", DisplayName: "Mistral", BaseURL: "https://api.mistral.ai/v1", EnvVars: []string{"MISTRAL_API_KEY", "MISTRALAI_API_KEY"}, Auth: AuthAPIKey},
	{ID: "cerebras", DisplayName: "Cerebras", BaseURL: "https://api.cerebras.ai/v1", EnvVars: []string{"CEREBRAS_API_KEY"}, Auth: AuthAPIKey},
	{ID: "groq", DisplayName: "Groq", BaseURL: "https://api.groq.com/openai/v1", EnvVars: []string{"GROQ_API_KEY"}, Auth: AuthAPIKey},
	{ID: "xai", DisplayName: "xAI (Grok)", BaseURL: "https://api.x.ai/v1", EnvVars: []string{"XAI_API_KEY"}, Auth: AuthAPIKey},
	{ID: "gemini", DisplayName: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", EnvVars: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, Auth: AuthAPIKey, CatalogID: "google"},
	{ID: "minimax", DisplayName: "MiniMax", BaseURL: "https://api.minimax.io/v1", EnvVars: []string{"MINIMAX_API_KEY"}, Auth: AuthAPIKey},

	// Ollama ships in two flavours. The local server needs no credentials and
	// serves whatever models the user has pulled; the cloud service takes an API
	// key and exposes a set of hosted models. They are separate providers so the
	// user can enable/disable each independently (e.g. hide local models).
	{ID: "ollama", DisplayName: "Ollama (local)", BaseURL: "http://localhost:11434/v1", Auth: AuthNone, LiveModels: true},
	{ID: "ollama-cloud", DisplayName: "Ollama (cloud)", BaseURL: "https://ollama.com/v1", EnvVars: []string{"OLLAMA_API_KEY"}, Auth: AuthAPIKey},

	// Anthropic speaks its native Messages API (not OpenAI-compatible), so it
	// uses the dedicated "anthropic" runtime class with an x-api-key. Base URL
	// is the host without /v1 — the anthropic client appends /v1/messages.
	{ID: "anthropic", DisplayName: "Anthropic (Claude)", BaseURL: "https://api.anthropic.com", EnvVars: []string{"ANTHROPIC_API_KEY"}, Auth: AuthAPIKey, Class: "anthropic"},
}

// Catalog returns a copy of the built-in provider catalog.
func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup returns the catalog entry with the given ID, case-insensitively.
func Lookup(id string) (CatalogEntry, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, e := range catalog {
		if e.ID == id {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// DetectEnvVar returns the first of the entry's candidate environment variables
// that is set (non-empty) according to getenv, along with true. If none are set
// it returns "", false. getenv defaults to os.Getenv semantics; callers pass it
// explicitly so the resolver stays testable.
func (e CatalogEntry) DetectEnvVar(getenv func(string) string) (string, bool) {
	for _, name := range e.EnvVars {
		if strings.TrimSpace(getenv(name)) != "" {
			return name, true
		}
	}
	// No key set, but for an API-key provider the first candidate is still the
	// variable the user would populate, useful for display.
	if len(e.EnvVars) > 0 {
		return e.EnvVars[0], false
	}
	return "", false
}
