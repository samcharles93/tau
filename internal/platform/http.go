package platform

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewHTTPClient returns an *http.Client configured for Tau providers.
// It respects the insecure flag for TLS verification and sets sensible timeouts.
// Proxy is disabled so provider requests use direct connections by default.
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
