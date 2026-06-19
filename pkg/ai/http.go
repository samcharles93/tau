package ai

import (
	"crypto/tls"
	"net/http"
	"time"
)

// newHTTPClient returns an *http.Client suitable for ai-sdk providers.
// It matches tau's existing behaviour: optional TLS verification skip and
// no proxy by default.
func newHTTPClient(insecure bool, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{
		Transport: &headerInjectingTransport{
			base: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
				Proxy:           nil,
			},
		},
		Timeout: timeout,
	}
}

// headerInjectingTransport pulls extra headers from the request context
// (see WithExtraHeaders) and adds them to the outgoing request. This lets
// tau's plugin lifecycle hooks (before_llm_call) add headers without the
// ai-sdk providers needing to know about them.
type headerInjectingTransport struct {
	base http.RoundTripper
}

func (t *headerInjectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if headers, ok := extraHeadersFrom(req.Context()); ok {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}
	return t.base.RoundTrip(req)
}
