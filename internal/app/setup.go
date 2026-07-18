package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
)

// ErrSetupCanceled is returned by RunSetup when the interactive flow is
// canceled (e.g. Ctrl+C) at any step. Callers (e.g. the CLI) can match on
// this to distinguish a clean user cancellation from a real failure.
var ErrSetupCanceled = errors.New("setup canceled")

// setupMaxModelOptions caps the number of models offered in the default-model
// picker. Some catalog providers (e.g. OpenRouter) expose far more
// tool-capable models than fit in a usable numbered CLI list.
const setupMaxModelOptions = 20

// SetupOption is one entry a SetupPrompter presents for the user to choose
// from. It mirrors internal/cli's SelectOption without this package
// depending on internal/cli, which already depends on this package.
type SetupOption struct {
	Label string
	Value string
}

// SetupPrompter is the interactive surface RunSetup needs: pick one of a
// list of options, or read a hidden secret. Implemented by the CLI layer so
// this package stays free of any terminal/UI dependency. Implementations
// must translate their own cancellation signal (e.g. Ctrl+C) into
// ErrSetupCanceled.
type SetupPrompter interface {
	Select(ctx context.Context, title string, options []SetupOption) (SetupOption, error)
	ReadSecret(ctx context.Context, prompt string) (string, error)
}

// RunSetupOptions holds the parameters for RunSetup.
type RunSetupOptions struct {
	// Prompter drives the interactive provider/model selection and API-key
	// entry. Required.
	Prompter SetupPrompter
	// Stdout receives plain informational output (OAuth device-code
	// instructions, progress notes). Defaults to os.Stdout.
	Stdout io.Writer
	// ProjectDir is passed through to config.SaveDefaultProviderAndModel;
	// empty uses the current working directory.
	ProjectDir string
	Insecure   bool
}

// SetupResult summarizes a successful RunSetup call, for a caller to print
// confirmation/guidance without re-deriving what was just configured.
type SetupResult struct {
	ProviderID   string
	ProviderName string
	// Model is the selected default model ID, or empty if none was selected
	// (e.g. the provider had no discoverable tool-capable models).
	Model string
}

// RunSetup walks the interactive setup flow: select a provider, authenticate
// it (OAuth device-code, API-key entry, or keyless enable), discover its
// tool-capable models, select a default, and persist provider+model to the
// global config. No credential is written until authentication for the
// chosen provider actually succeeds, and nothing is written at all if the
// flow is canceled before then. See internal/providers/manage.go for the
// underlying state transitions.
func RunSetup(ctx context.Context, opts RunSetupOptions) (SetupResult, error) {
	if opts.Prompter == nil {
		return SetupResult{}, errors.New("setup: no prompter configured")
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	entry, err := setupSelectProvider(ctx, opts)
	if err != nil {
		return SetupResult{}, err
	}

	manage := providers.NewManage(nil)
	switch entry.Auth {
	case providers.AuthOAuth:
		if err := authenticateProviderOAuth(ctx, stdout, manage, entry, "", providers.LoginOAuth); err != nil {
			return SetupResult{}, err
		}
	case providers.AuthAPIKey:
		if err := authenticateProviderAPIKey(ctx, opts, manage, entry); err != nil {
			return SetupResult{}, err
		}
	default: // providers.AuthNone: keyless, e.g. a local Ollama server.
		if err := manage.Enable(entry.ID); err != nil {
			return SetupResult{}, fmt.Errorf("enable %s: %w", entry.DisplayName, err)
		}
	}

	providerCfg, err := setupResolveProviderConfig(ctx, manage, entry.ID)
	if err != nil {
		return SetupResult{}, err
	}

	modelID, err := setupSelectModel(ctx, opts, providerCfg)
	if err != nil {
		return SetupResult{}, err
	}

	if err := tauconfig.SaveDefaultProviderAndModel(opts.ProjectDir, entry.ID, modelID); err != nil {
		return SetupResult{}, fmt.Errorf("save default provider/model: %w", err)
	}
	return SetupResult{ProviderID: entry.ID, ProviderName: entry.DisplayName, Model: modelID}, nil
}

func setupSelectProvider(ctx context.Context, opts RunSetupOptions) (providers.CatalogEntry, error) {
	cfg, err := tauconfig.LoadConfigAllowEmpty()
	if err != nil {
		return providers.CatalogEntry{}, fmt.Errorf("load config: %w", err)
	}
	state, err := providers.LoadState()
	if err != nil {
		return providers.CatalogEntry{}, fmt.Errorf("load provider state: %w", err)
	}
	menu := providers.Menu(cfg, state, nil)

	options := make([]SetupOption, 0, len(menu))
	for _, e := range menu {
		label := e.DisplayName
		if e.Enabled {
			label += " (already enabled)"
		}
		options = append(options, SetupOption{Label: label, Value: e.ID})
	}

	choice, err := opts.Prompter.Select(ctx, "Select a provider", options)
	if err != nil {
		return providers.CatalogEntry{}, setupPromptError(err)
	}
	entry, ok := providers.Lookup(choice.Value)
	if !ok {
		return providers.CatalogEntry{}, fmt.Errorf("unknown provider %q", choice.Value)
	}
	return entry, nil
}

func authenticateProviderOAuth(ctx context.Context, stdout io.Writer, manage *providers.Manage, entry providers.CatalogEntry, enterpriseDomain string, loginOAuth oauthLoginFunc) error {
	_, _ = fmt.Fprintln(stdout, providerui.StartMessage(entry.DisplayName))
	creds, err := loginOAuth(ctx, entry.ID, providers.LoginOptions{
		EnterpriseDomain: strings.TrimSpace(enterpriseDomain),
		OnDeviceCode: func(code providers.DeviceCode) {
			opened := providerui.TryOpenBrowser(ctx, code.VerificationURI)
			_, _ = fmt.Fprintln(stdout, providerui.DeviceCodeMessage(entry.DisplayName, code, opened, false))
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ErrSetupCanceled
		}
		return fmt.Errorf("%s", providerui.FailureMessage(entry.DisplayName, err))
	}
	if err := manage.LoginComplete(entry.ID, creds); err != nil {
		return fmt.Errorf("complete %s login: %w", entry.DisplayName, err)
	}
	return nil
}

// apiKeyRetryOption re-prompts for a fresh key; apiKeyOverrideOption stores
// the just-entered key despite a definite rejection; apiKeyProceedOption
// stores it despite an inconclusive validation result. Shared across both
// choice prompts below so the retry branch is identical either way.
const (
	apiKeyRetryOption    = "retry"
	apiKeyOverrideOption = "override"
	apiKeyProceedOption  = "proceed"
)

func authenticateProviderAPIKey(ctx context.Context, opts RunSetupOptions, manage *providers.Manage, entry providers.CatalogEntry) error {
	for {
		key, err := opts.Prompter.ReadSecret(ctx, fmt.Sprintf("Paste your API key for %s (input hidden): ", entry.DisplayName))
		if err != nil {
			return setupPromptError(err)
		}
		if strings.TrimSpace(key) == "" {
			continue // re-prompt; only Ctrl+C (already handled above) exits this loop
		}

		switch result := validateAPIKey(ctx, entry, key, opts.Insecure); result.outcome {
		case apiKeyValid:
			// fall through to store below
		case apiKeyRejected:
			choice, err := opts.Prompter.Select(ctx, apiKeyRejectedPrompt(entry, result.err), []SetupOption{
				{Label: "Try a different key", Value: apiKeyRetryOption},
				{Label: "Use this key anyway (not recommended)", Value: apiKeyOverrideOption},
			})
			if err != nil {
				return setupPromptError(err)
			}
			if choice.Value == apiKeyRetryOption {
				continue
			}
		case apiKeyInconclusive:
			choice, err := opts.Prompter.Select(ctx, apiKeyInconclusivePrompt(entry, result.err), []SetupOption{
				{Label: "Continue without verifying", Value: apiKeyProceedOption},
				{Label: "Try a different key", Value: apiKeyRetryOption},
			})
			if err != nil {
				return setupPromptError(err)
			}
			if choice.Value == apiKeyRetryOption {
				continue
			}
		}

		if err := manage.StoreAPIKey(entry.ID, key); err != nil {
			return fmt.Errorf("store %s API key: %w", entry.DisplayName, err)
		}
		return nil
	}
}

// apiKeyRejectedPrompt formats a clear, actionable message for a definite
// key rejection (401/403): the provider said this credential is wrong, not
// "something went wrong". err is safe to include - it never contains the key
// (see liveValidateAPIKey).
func apiKeyRejectedPrompt(entry providers.CatalogEntry, err error) string {
	detail := "the provider rejected it"
	if err != nil {
		detail = err.Error()
	}
	return fmt.Sprintf("%s rejected this API key (%s). It looks incorrect, expired, or revoked.", entry.DisplayName, detail)
}

// apiKeyInconclusivePrompt formats a warning for a validation attempt that
// could not reach a definite answer (network error, timeout, provider
// outage, unexpected status) - the key might still be valid.
func apiKeyInconclusivePrompt(entry providers.CatalogEntry, err error) string {
	detail := "an unknown error"
	if err != nil {
		detail = err.Error()
	}
	return fmt.Sprintf("Could not verify this %s API key (%s). The key may still be valid - this could be a network issue.", entry.DisplayName, detail)
}

// setupResolveProviderConfig re-resolves the effective provider set after
// authentication and returns the config.ProviderConfig tau will actually use
// for id, so model discovery sees the exact same BaseURL/Auth/Headers the
// runtime would.
func setupResolveProviderConfig(ctx context.Context, manage *providers.Manage, id string) (tauconfig.ProviderConfig, error) {
	_, resolved, err := manage.Effective(ctx)
	if err != nil {
		return tauconfig.ProviderConfig{}, fmt.Errorf("resolve effective providers: %w", err)
	}
	for _, r := range resolved {
		if r.Config.Name == id {
			return r.Config, nil
		}
	}
	return tauconfig.ProviderConfig{}, fmt.Errorf("%s did not resolve after authentication", id)
}

// setupSelectModel discovers tool-capable models for providerCfg and prompts
// for a default. An empty result (discovery failure or a provider with no
// tool-capable models in the embedded snapshot) is not a hard failure: the
// provider is already validly configured at this point, so setup still
// succeeds without a default_model set.
func setupSelectModel(ctx context.Context, opts RunSetupOptions, providerCfg tauconfig.ProviderConfig) (string, error) {
	rt := newRuntimeForProviders([]tauconfig.ProviderConfig{providerCfg}, opts.Insecure)
	refs := aggregateModelRefs(ctx, rt, opts.Insecure, []tauconfig.ProviderConfig{providerCfg})
	if len(refs) == 0 {
		return "", nil
	}
	if len(refs) > setupMaxModelOptions {
		refs = refs[:setupMaxModelOptions]
	}

	options := make([]SetupOption, 0, len(refs))
	for _, ref := range refs {
		options = append(options, SetupOption{Label: ref.ID, Value: ref.ID})
	}
	choice, err := opts.Prompter.Select(ctx, "Select a default model", options)
	if err != nil {
		return "", setupPromptError(err)
	}
	return choice.Value, nil
}

func setupPromptError(err error) error {
	if errors.Is(err, ErrSetupCanceled) {
		return err
	}
	return fmt.Errorf("setup: %w", err)
}
