package platform

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewHTTPClient returns an *http.Client configured for the AIM platform.
// It respects the insecure flag for TLS verification and sets sensible timeouts.
// Proxy is disabled — AIM endpoints must be reached directly, not via Zscaler
// proxy (per AIM troubleshooting: "Connection reset by peer → prefix with
// HTTPS_PROXY="" HTTP_PROXY=""").
func NewHTTPClient(insecure bool) *http.Client {
	return NewHTTPClientWithTimeout(insecure, 30*time.Second)
}

func NewHTTPClientWithTimeout(insecure bool, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure,
		},
		Proxy: nil,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
