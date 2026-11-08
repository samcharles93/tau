package cli

import (
	"errors"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/providers"
)

func TestUnavailableProviderErrorExplainsMissingEnvVar(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{
			Config:    config.ProviderConfig{Name: "anthropic"},
			Source:    providers.SourceEnv,
			Available: false,
			Message:   "ANTHROPIC_API_KEY is not set",
		},
	}

	got := unavailableProviderError(resolved, "anthropic", "")
	want := `provider "anthropic" is configured but not currently usable: ANTHROPIC_API_KEY is not set`
	if got != want {
		t.Errorf("unavailableProviderError() = %q, want %q", got, want)
	}
}

func TestUnavailableProviderErrorFallsBackToDefaultProvider(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "anthropic"}, Available: false, Message: "ANTHROPIC_API_KEY is not set"},
	}

	// No --provider flag passed (requested == ""), so it should fall back to
	// checking cfg.DefaultProvider instead.
	got := unavailableProviderError(resolved, "", "anthropic")
	if got == "" {
		t.Fatal("expected a message when falling back to the default provider")
	}
}

func TestUnavailableProviderErrorEmptyForUnknownName(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "anthropic"}, Available: false, Message: "ANTHROPIC_API_KEY is not set"},
	}

	// A genuinely unrecognized name must not match anything, so the caller
	// falls back to the generic "unknown provider" error instead of a
	// confusing unrelated message.
	got := unavailableProviderError(resolved, "totally-made-up", "")
	if got != "" {
		t.Errorf("unavailableProviderError() = %q, want empty for an unrecognized name", got)
	}
}

func TestUnavailableProviderErrorEmptyWhenAvailable(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "openai"}, Available: true},
	}

	// An available provider should never produce an "unavailable" message -
	// if ResolveProvider still failed for it, that's a genuine bug elsewhere,
	// not a missing-credential story.
	got := unavailableProviderError(resolved, "openai", "")
	if got != "" {
		t.Errorf("unavailableProviderError() = %q, want empty for an available provider", got)
	}
}

func TestUnavailableProviderErrorDefaultsMessageWhenEmpty(t *testing.T) {
	resolved := []providers.ResolvedProvider{
		{Config: config.ProviderConfig{Name: "mystery"}, Available: false, Message: ""},
	}

	got := unavailableProviderError(resolved, "mystery", "")
	want := `provider "mystery" is configured but not currently usable: credential not available`
	if got != want {
		t.Errorf("unavailableProviderError() = %q, want %q", got, want)
	}
}

func TestFormatConfigReportNoIssues(t *testing.T) {
	got := formatConfigReport(nil)
	if got != "Config check: no issues found.\n" {
		t.Errorf("formatConfigReport(nil) = %q", got)
	}
}

func TestFormatConfigReportListsEveryIssue(t *testing.T) {
	issues := []config.ConfigIssue{
		{Severity: config.IssueSeverityError, Message: `provider "x" base_url is required`},
		{Severity: config.IssueSeverityWarning, Message: `default_provider "ghost" is not in the providers list`},
		{Severity: config.IssueSeverityError, Message: `updates.mode: unsupported mode "bogus" (valid: warn, disabled, auto)`},
	}
	got := formatConfigReport(issues)
	want := "Config check: 3 issue(s) - 2 error(s), 1 warning(s)\n" +
		"  [error] provider \"x\" base_url is required\n" +
		"  [warning] default_provider \"ghost\" is not in the providers list\n" +
		"  [error] updates.mode: unsupported mode \"bogus\" (valid: warn, disabled, auto)\n"
	if got != want {
		t.Errorf("formatConfigReport() = %q, want %q", got, want)
	}
}

func TestRunConfigCheckExitCode(t *testing.T) {
	var buf strings.Builder

	// Valid config: report printed, nil error (exit 0).
	valid := config.Config{
		Providers: []config.ProviderConfig{
			{Name: "local", BaseURL: "http://localhost:11434", Auth: config.AuthConfig{Type: config.AuthTypeNone}},
		},
	}
	if err := runConfigCheck(valid, &buf); err != nil {
		t.Fatalf("runConfigCheck(valid) error = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "no issues found") {
		t.Errorf("report = %q, want 'no issues found'", buf.String())
	}

	// Broken config: report lists ALL errors and the command exits 1.
	buf.Reset()
	broken := config.Config{
		Providers: []config.ProviderConfig{
			{Name: "a", BaseURL: "", Auth: config.AuthConfig{Type: config.AuthTypeNone}},
			{Name: "b", BaseURL: "https://b.example", Auth: config.AuthConfig{Type: "kerberos"}},
		},
		Updates: config.UpdatesConfig{Mode: "bogus"},
	}
	err := runConfigCheck(broken, &buf)
	if err == nil {
		t.Fatal("runConfigCheck(broken) error = nil, want exit error")
	}
	var exitErr urfavecli.ExitCoder
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T, want urfavecli.ExitCoder", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}
	report := buf.String()
	for _, want := range []string{"3 issue(s)", "base_url is required", "unsupported auth type", "unsupported mode"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestConfigCmdRegisteredOnRoot(t *testing.T) {
	root := NewRootCommand("test")
	found := false
	for _, sub := range root.Commands {
		if sub.Name == "config" {
			found = true
			requireConfigCheckSubcommand(t, sub)
		}
	}
	if !found {
		t.Fatal("root command has no 'config' subcommand")
	}
}

func requireConfigCheckSubcommand(t *testing.T, cmd *urfavecli.Command) {
	t.Helper()
	for _, sub := range cmd.Commands {
		if sub.Name == "check" {
			if len(sub.Aliases) == 0 || sub.Aliases[0] != "validate" {
				t.Errorf("config check aliases = %v, want [validate]", sub.Aliases)
			}
			return
		}
	}
	t.Error("config command has no 'check' subcommand")
}
