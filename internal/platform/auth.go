package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
	"github.com/pkg/browser"
)

const (
	oauthClientID = "openshift-cli-client"
	oauthIDP      = "oneaccess"
	authTimeout   = 5 * time.Minute
)

// tokenCache stores cached OCP tokens on disk.
type tokenCache struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// cacheFilePath returns the path to the token cache file for a given endpoint.
func cacheFilePath(ocpAPI string) string {
	hash := sha256.Sum256([]byte(ocpAPI))
	name := hex.EncodeToString(hash[:16]) + ".json"
	return filepath.Join(aimconfig.Dir(), "cache", name)
}

// loadCachedToken attempts to load a cached OCP token.
func loadCachedToken(ocpAPI string) (*tokenCache, error) {
	data, err := os.ReadFile(cacheFilePath(ocpAPI))
	if err != nil {
		return nil, err
	}

	var cache tokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// saveCachedToken persists an OCP token to disk.
func saveCachedToken(ocpAPI string, cache *tokenCache) error {
	dir := filepath.Join(aimconfig.Dir(), "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}

	return os.WriteFile(cacheFilePath(ocpAPI), data, 0o600)
}

// randomURLSafe generates a URL-safe base64 random string.
func randomURLSafe(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// pkcePair holds a PKCE verifier and its S256 challenge.
type pkcePair struct {
	Verifier  string
	Challenge string
}

// newPKCE generates a PKCE verifier/challenge pair (matching the PS1 script).
func newPKCE() (*pkcePair, error) {
	verifier, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return &pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

// GetOCPToken obtains an OCP access token via cache or browser PKCE flow.
// Implements the same OAuth flow as the PowerShell oc-login.ps1 script:
// 1. Bind localhost callback server on 127.0.0.1:<random>/callback
// 2. Open browser to authorize endpoint with PKCE + IDP hint
// 3. Capture auth code from callback
// 4. Exchange code for token with HTTP Basic auth (client_id: empty password)
func GetOCPToken(ctx context.Context, endpoint Endpoint, insecure bool) (string, error) {
	// 1. Check cache
	cached, err := loadCachedToken(endpoint.OCPAPI)
	if err == nil && cached.AccessToken != "" && time.Now().Before(cached.Expiry) {
		slog.Debug("using cached OCP token", "expires", cached.Expiry.Format(time.RFC3339))
		return cached.AccessToken, nil
	}

	return runBrowserAuthFlow(ctx, endpoint, insecure)
}

// GetFreshOCPToken forces a new browser auth flow, bypassing the cache.
// Call this when a cached token was rejected (e.g. 401 from MaaS gateway).
func GetFreshOCPToken(ctx context.Context, endpoint Endpoint, insecure bool) (string, error) {
	InvalidateCache(endpoint.OCPAPI)
	return runBrowserAuthFlow(ctx, endpoint, insecure)
}

// InvalidateCache removes the cached token file for the given endpoint.
func InvalidateCache(ocpAPI string) {
	path := cacheFilePath(ocpAPI)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not remove token cache", "path", path, "err", err)
	}
}

// runBrowserAuthFlow performs the full OAuth PKCE browser login.
func runBrowserAuthFlow(ctx context.Context, endpoint Endpoint, insecure bool) (string, error) {
	// 2. Generate PKCE pair and state
	pkce, err := newPKCE()
	if err != nil {
		return "", fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := randomURLSafe(16)
	if err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	// 3. Start local callback server on 127.0.0.1:0 (random port)
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("binding callback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	// OCP's openshift-cli-client requires redirect_uri starting with http://127.0.0.1
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// 4. Build authorize URL (matching PowerShell script exactly)
	params := url.Values{
		"client_id":             {oauthClientID},
		"response_type":         {"code"},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"idp":                   {oauthIDP},
	}
	authURL := endpoint.OAuthAuthorizeURL() + "?" + params.Encode()

	// 5. Open browser
	slog.Debug("opening browser for OAuth login", "idp", oauthIDP)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser. Visit:\n  %s\n", authURL)
	}

	// 6. Wait for callback with auth code
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		code := query.Get("code")
		callbackState := query.Get("state")
		errDesc := query.Get("error_description")
		if errDesc == "" {
			errDesc = query.Get("error")
		}

		if code != "" && callbackState == state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h2>Login complete — you can close this window.</h2><script>window.close();</script></body></html>`)
			resultCh <- callbackResult{code: code}
			return
		}

		msg := "state mismatch"
		if errDesc != "" {
			msg = errDesc
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<html><body><h2>Login failed: %s</h2></body></html>`, msg)
		resultCh <- callbackResult{err: fmt.Errorf("OAuth callback error: %s", msg)}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("callback server stopped unexpectedly", "err", err)
		}
	}()

	// Wait for result or timeout
	ctx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()

	var result callbackResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		return "", fmt.Errorf("OAuth login timed out (waited %s)", authTimeout)
	}
	_ = server.Close()

	if result.err != nil {
		return "", result.err
	}

	// 7. Exchange code for token (HTTP Basic auth with empty password, matching PS1)
	accessToken, err := exchangeCodeForToken(ctx, endpoint, result.code, redirectURI, pkce.Verifier, insecure)
	if err != nil {
		return "", err
	}

	// 8. Cache the token (OCP tokens are valid for ~24h by default)
	cache := &tokenCache{
		AccessToken: accessToken,
		Expiry:      time.Now().Add(23 * time.Hour),
	}
	if err := saveCachedToken(endpoint.OCPAPI, cache); err != nil {
		slog.Warn("could not cache token", "err", err)
	}

	slog.Debug("OCP token obtained")
	return accessToken, nil
}

// exchangeCodeForToken exchanges an authorization code for an access token.
// Uses HTTP Basic auth (client_id with empty password) as required by OCP OAuth.
func exchangeCodeForToken(ctx context.Context, endpoint Endpoint, code, redirectURI, codeVerifier string, insecure bool) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.OAuthTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// HTTP Basic: base64("openshift-cli-client:") — empty password, required by OCP
	req.SetBasicAuth(oauthClientID, "")

	client := NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("token exchange failed: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token exchange returned no access_token")
	}

	return tokenResp.AccessToken, nil
}
