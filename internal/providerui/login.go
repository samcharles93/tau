package providerui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/pkg/browser"

	"github.com/samcharles93/tau/internal/providers"
)

// StartMessage is the first scrollback line for a provider OAuth login.
func StartMessage(displayName string) string {
	return fmt.Sprintf("%s login\n\n  Starting device authorization...", displayName)
}

// DeviceCodeMessage renders the browser/device-code step. opened reports
// whether Tau successfully asked the OS to open the verification URL; copied
// reports whether Tau attempted to copy the user code to the clipboard.
func DeviceCodeMessage(displayName string, code providers.DeviceCode, opened, copied bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s authorization\n\n", displayName)
	if opened {
		b.WriteString("  1. Browser opened\n")
	} else {
		b.WriteString("  1. Open this URL\n")
	}
	if strings.TrimSpace(code.VerificationURI) != "" {
		fmt.Fprintf(&b, "     %s\n", code.VerificationURI)
	}
	if strings.TrimSpace(code.UserCode) != "" {
		if copied {
			fmt.Fprintf(&b, "\n  2. Paste code (copied)\n     %s\n", code.UserCode)
		} else {
			fmt.Fprintf(&b, "\n  2. Enter code\n     %s\n", code.UserCode)
		}
	}
	b.WriteString("\n  Waiting for authorization...")
	return b.String()
}

// FailureMessage formats a provider OAuth failure as a short block instead of
// a long single scrollback line.
func FailureMessage(displayName string, err error) string {
	if err == nil {
		return fmt.Sprintf("%s login failed", displayName)
	}
	return fmt.Sprintf("%s login failed\n\n  %s", displayName, sentenceLines(err.Error()))
}

// TryOpenBrowser attempts to open url in the user's default browser. Login
// must not depend on this working: SSH, tmux, and headless terminals commonly
// cannot open a local browser.
func TryOpenBrowser(ctx context.Context, url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}
	return openURL(ctx, url) == nil
}

var openURL = defaultOpenURL

var browserMu sync.Mutex

func defaultOpenURL(ctx context.Context, url string) error {
	_ = ctx
	return withDiscardedBrowserOutput(func() error {
		return browser.OpenURL(url)
	})
}

func withDiscardedBrowserOutput(open func() error) error {
	browserMu.Lock()
	defer browserMu.Unlock()

	oldStdout := browser.Stdout
	oldStderr := browser.Stderr
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	defer func() {
		browser.Stdout = oldStdout
		browser.Stderr = oldStderr
	}()
	return open()
}

func sentenceLines(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "unknown error"
	}
	return strings.ReplaceAll(text, ". ", ".\n  ")
}
