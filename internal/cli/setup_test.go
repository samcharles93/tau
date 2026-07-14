package cli

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/app"
)

func TestSetupExitPlanSuccessWithModel(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{ProviderID: "deepseek", ProviderName: "DeepSeek", Model: "deepseek-chat"}, nil)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdoutMsg, "DeepSeek")
	assert.Contains(t, stdoutMsg, "deepseek-chat")
	assert.Empty(t, stderrMsg)
}

func TestSetupExitPlanSuccessWithoutModel(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{ProviderID: "deepseek", ProviderName: "DeepSeek"}, nil)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdoutMsg, "DeepSeek")
	assert.Contains(t, stdoutMsg, "/model")
	assert.Empty(t, stderrMsg)
}

func TestSetupExitPlanCanceled(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{}, app.ErrSetupCanceled)
	assert.Equal(t, 130, code)
	assert.Empty(t, stdoutMsg)
	assert.Contains(t, stderrMsg, "canceled")
}

func TestSetupExitPlanFailure(t *testing.T) {
	code, stdoutMsg, stderrMsg := setupExitPlan(app.SetupResult{}, errors.New("boom"))
	assert.Equal(t, 1, code)
	assert.Empty(t, stdoutMsg)
	assert.Contains(t, stderrMsg, "boom")
}

func TestCliSetupPrompterSelectTranslatesCancellation(t *testing.T) {
	// cliSetupPrompter.Select reads os.Stdin directly, so exercising its
	// cancellation path (rather than Select's, already covered in
	// prompt_test.go) means swapping os.Stdin for a pipe with no writer —
	// the read would block forever if ctx cancellation didn't win the race.
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pw.Close()
	origStdin := os.Stdin
	os.Stdin = pr
	t.Cleanup(func() { os.Stdin = origStdin })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := cliSetupPrompter{}.Select(ctx, "Providers", []app.SetupOption{{Label: "a", Value: "a"}})
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, app.ErrSetupCanceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Select did not return promptly after context cancellation")
	}
}

func TestCliSetupPrompterReadSecretTranslatesCancellation(t *testing.T) {
	original := readPassword
	t.Cleanup(func() { readPassword = original })
	block := make(chan struct{})
	readPassword = func(fd int) ([]byte, error) {
		<-block
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := cliSetupPrompter{}.ReadSecret(ctx, "")
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, app.ErrSetupCanceled)
	case <-time.After(2 * time.Second):
		t.Fatal("ReadSecret did not return promptly after context cancellation")
	}
}
