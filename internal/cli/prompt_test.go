package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerOptions() []SelectOption {
	return []SelectOption{
		{Label: "Anthropic (Claude)", Value: "anthropic"},
		{Label: "OpenAI", Value: "openai"},
		{Label: "Ollama (local)", Value: "ollama"},
	}
}

func TestSelectRendersNumberedListAndPicksValidChoice(t *testing.T) {
	var out strings.Builder
	got, err := Select(context.Background(), &out, strings.NewReader("2\n"), "Providers", providerOptions())
	require.NoError(t, err)
	assert.Equal(t, "openai", got.Value)

	rendered := out.String()
	assert.Contains(t, rendered, "1) Anthropic (Claude)")
	assert.Contains(t, rendered, "2) OpenAI")
	assert.Contains(t, rendered, "3) Ollama (local)")
}

func TestSelectRepromptsOnNonNumericThenOutOfRange(t *testing.T) {
	var out strings.Builder
	got, err := Select(context.Background(), &out, strings.NewReader("abc\n99\n1\n"), "Providers", providerOptions())
	require.NoError(t, err)
	assert.Equal(t, "anthropic", got.Value)

	rendered := out.String()
	if n := strings.Count(rendered, "please enter a number between 1 and 3"); n != 2 {
		t.Fatalf("expected 2 re-prompts (non-numeric, then out-of-range), got %d in:\n%s", n, rendered)
	}
}

func TestSelectEmptyOptionsErrors(t *testing.T) {
	var out strings.Builder
	_, err := Select(context.Background(), &out, strings.NewReader("1\n"), "", nil)
	require.Error(t, err)
}

func TestSelectEOFReturnsError(t *testing.T) {
	var out strings.Builder
	_, err := Select(context.Background(), &out, strings.NewReader(""), "Providers", providerOptions())
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
}

func TestSelectCanceledContextExitsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pr, pw := io.Pipe()
	defer pw.Close()

	var out strings.Builder
	done := make(chan struct{})
	var err error
	go func() {
		_, err = Select(ctx, &out, pr, "Providers", providerOptions())
		close(done)
	}()

	select {
	case <-done:
		require.ErrorIs(t, err, ErrPromptCanceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Select did not return promptly after context cancellation")
	}
}

func TestReadSecretReturnsHiddenValue(t *testing.T) {
	original := readPassword
	t.Cleanup(func() { readPassword = original })
	readPassword = func(fd int) ([]byte, error) {
		return []byte("sk-test-key"), nil
	}

	var out strings.Builder
	got, err := ReadSecret(context.Background(), &out, 0, "API key: ")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key", got)
	assert.Contains(t, out.String(), "API key: ")
	assert.NotContains(t, out.String(), "sk-test-key", "the secret itself must never be echoed to the writer")
}

func TestReadSecretPropagatesUnderlyingError(t *testing.T) {
	original := readPassword
	t.Cleanup(func() { readPassword = original })
	wantErr := errors.New("not a terminal")
	readPassword = func(fd int) ([]byte, error) {
		return nil, wantErr
	}

	var out strings.Builder
	_, err := ReadSecret(context.Background(), &out, 0, "")
	require.ErrorIs(t, err, wantErr)
}

func TestReadSecretCanceledContextExitsCleanly(t *testing.T) {
	original := readPassword
	t.Cleanup(func() { readPassword = original })
	block := make(chan struct{}) // never closed: simulates a still-blocked terminal read
	readPassword = func(fd int) ([]byte, error) {
		<-block
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out strings.Builder
	done := make(chan struct{})
	var err error
	go func() {
		_, err = ReadSecret(ctx, &out, 0, "")
		close(done)
	}()

	select {
	case <-done:
		require.ErrorIs(t, err, ErrPromptCanceled)
	case <-time.After(2 * time.Second):
		t.Fatal("ReadSecret did not return promptly after context cancellation")
	}
}

func TestNoColorSuppressesStyling(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	assert.Equal(t, "Providers", styleBold("Providers"))
}

func TestStyleBoldAddsEscapesWhenColorAllowed(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	styled := styleBold("Providers")
	assert.NotEqual(t, "Providers", styled)
	assert.Contains(t, styled, "Providers")
}
