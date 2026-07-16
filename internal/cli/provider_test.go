package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/app"
)

func TestRenderProviderListShowsStatusSourceAndAuth(t *testing.T) {
	var output bytes.Buffer
	renderProviderList(&output, []app.ProviderStatus{
		{ID: "openai", Status: "enabled", Source: "managed", Auth: "api_key", Details: "stored key"},
		{ID: "github-copilot", Status: "disabled", Source: "oauth", Auth: "oauth"},
	})

	text := output.String()
	for _, want := range []string{"PROVIDER", "STATUS", "SOURCE", "AUTH", "openai", "enabled", "managed", "api_key", "stored key", "github-copilot", "disabled", "oauth"} {
		assert.Contains(t, text, want)
	}
}

func TestProviderLoginNameRequiresNameOffTTY(t *testing.T) {
	_, err := providerLoginName(context.Background(), "", false, io.Discard, strings.NewReader(""), nil)
	require.ErrorIs(t, err, errProviderUsage)
	assert.Contains(t, err.Error(), "provider name is required")
}

func TestProviderLoginNameUsesInteractiveSelector(t *testing.T) {
	name, err := providerLoginName(
		context.Background(),
		"",
		true,
		io.Discard,
		strings.NewReader(""),
		func(_ context.Context, _ io.Writer, _ io.Reader, _ string, options []SelectOption) (SelectOption, error) {
			require.NotEmpty(t, options)
			return SelectOption{Value: "openai-codex"}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "openai-codex", name)
}

func TestProviderLoginNameNormalisesExplicitName(t *testing.T) {
	name, err := providerLoginName(context.Background(), " OpenAI-Codex ", false, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "openai-codex", name)
}

func TestProviderExitPlan(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{name: "success", code: 0},
		{name: "usage", err: providerUsage("usage: tau provider login <name>"), code: 2, msg: "usage"},
		{name: "cancel", err: ErrPromptCanceled, code: 130, msg: "canceled"},
		{name: "failure", err: errors.New("boom"), code: 1, msg: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := providerExitPlan("provider login", tt.err)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, msg, tt.msg)
		})
	}
}

func TestProviderCommandGroupContainsListLoginLogout(t *testing.T) {
	cmd := providerCmd()
	assert.Equal(t, "provider", cmd.Name)
	names := make([]string, 0, len(cmd.Commands))
	for _, sub := range cmd.Commands {
		names = append(names, sub.Name)
	}
	assert.ElementsMatch(t, []string{"list", "login", "logout"}, names)
}
