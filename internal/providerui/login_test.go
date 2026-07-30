package providerui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/browser"

	"github.com/samcharles93/tau/internal/providers"
)

func TestDeviceCodeMessageBrowserOpened(t *testing.T) {
	msg := DeviceCodeMessage("GitHub Copilot", providers.DeviceCode{
		VerificationURI: "https://github.com/login/device",
		UserCode:        "ABCD-1234",
	}, true, true)
	for _, want := range []string{
		"GitHub Copilot authorization",
		"1. Browser opened",
		"https://github.com/login/device",
		"2. Paste code (copied)",
		"ABCD-1234",
		"Waiting for authorization",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q to contain %q", msg, want)
		}
	}
}

func TestDeviceCodeMessageBrowserFallback(t *testing.T) {
	msg := DeviceCodeMessage("GitHub Copilot", providers.DeviceCode{
		VerificationURI: "https://github.com/login/device",
		UserCode:        "ABCD-1234",
	}, false, false)
	if !strings.Contains(msg, "1. Open this URL") {
		t.Fatalf("expected fallback URL step, got %q", msg)
	}
	if !strings.Contains(msg, "2. Enter code") {
		t.Fatalf("expected manual code step, got %q", msg)
	}
	if strings.Contains(msg, "Browser opened") {
		t.Fatalf("did not expect browser-opened text, got %q", msg)
	}
}

func TestFailureMessageSplitsSentences(t *testing.T) {
	msg := FailureMessage("GitHub Copilot", errors.New("first sentence. second sentence. final"))
	for _, want := range []string{
		"GitHub Copilot login failed",
		"first sentence.\n  second sentence.\n  final",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q to contain %q", msg, want)
		}
	}
}

func TestTryOpenBrowser(t *testing.T) {
	oldOpenURL := openURL
	defer func() { openURL = oldOpenURL }()

	var gotURL string
	openURL = func(_ context.Context, url string) error {
		gotURL = url
		return nil
	}
	if !TryOpenBrowser(context.Background(), " https://example.com/login ") {
		t.Fatal("expected browser open to succeed")
	}
	if gotURL != "https://example.com/login" {
		t.Fatalf("opened URL %q, want trimmed URL", gotURL)
	}

	openURL = func(context.Context, string) error { return errors.New("not available") }
	if TryOpenBrowser(context.Background(), "https://example.com/login") {
		t.Fatal("expected browser open failure to return false")
	}
	if TryOpenBrowser(context.Background(), " ") {
		t.Fatal("expected blank URL to return false")
	}
}

func TestWithDiscardedBrowserOutputSuppressesCommandNoise(t *testing.T) {
	oldStdout := browser.Stdout
	oldStderr := browser.Stderr
	defer func() {
		browser.Stdout = oldStdout
		browser.Stderr = oldStderr
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	browser.Stdout = &stdout
	browser.Stderr = &stderr

	err := withDiscardedBrowserOutput(func() error {
		fmt.Fprint(browser.Stdout, "stdout noise")
		fmt.Fprint(browser.Stderr, "stderr noise")
		return nil
	})
	if err != nil {
		t.Fatalf("withDiscardedBrowserOutput() error = %v", err)
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("expected browser output to be discarded, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
