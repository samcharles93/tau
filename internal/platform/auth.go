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

	"github.com/pkg/browser"
	"github.com/samcharles93/tau/internal/config"
)

const authTimeout = 5 * time.Minute

// tokenCache stores cached OAuth tokens on disk.
type tokenCache struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// ResolveBearerToken returns a bearer token for the provider auth configuration.
func ResolveBearerToken(ctx context.Context, provider config.ProviderConfig, insecure bool) (string, error) {
	switch strings.TrimSpace(provider.Auth.Type) {
	case config.AuthTypeAPIKey:
		return apiKeyToken(provider)
	case config.AuthTypeNone:
		return "", nil
	case config.AuthTypeOAuthPKCE:
		return GetOAuthToken(ctx, provider, insecure)
	default:
		return "", fmt.Errorf("provider %q has unsupported auth type %q", provider.Name, provider.Auth.Type)
	}
}

func apiKeyToken(provider config.ProviderConfig) (string, error) {
	if envName := strings.TrimSpace(provider.Auth.APIKeyEnv); envName != "" {
		if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
			return token, nil
		}
		if strings.TrimSpace(provider.Auth.APIKey) == "" {
			return "", fmt.Errorf("provider %q api key environment variable %s is not set", provider.Name, envName)
		}
	}
	if token := strings.TrimSpace(provider.Auth.APIKey); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("provider %q api_key auth requires api_key_env or api_key", provider.Name)
}

func cacheFilePath(provider config.ProviderConfig) string {
	key := provider.Name + "\x00" + provider.Auth.AuthorizeURL + "\x00" + provider.Auth.TokenURL + "\x00" + provider.Auth.ClientID
	hash := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(hash[:16]) + ".json"
	return filepath.Join(config.Dir(), "cache", name)
}

func loadCachedToken(provider config.ProviderConfig) (*tokenCache, error) {
	data, err := os.ReadFile(cacheFilePath(provider))
	if err != nil {
		return nil, err
	}

	var cache tokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveCachedToken(provider config.ProviderConfig, cache *tokenCache) error {
	dir := filepath.Join(config.Dir(), "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(cacheFilePath(provider), data, 0o600)
}

func randomURLSafe(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

type pkcePair struct {
	Verifier  string
	Challenge string
}

func newPKCE() (*pkcePair, error) {
	verifier, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	return &pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

// GetOAuthToken obtains an access token via cache or browser PKCE flow.
func GetOAuthToken(ctx context.Context, provider config.ProviderConfig, insecure bool) (string, error) {
	cached, err := loadCachedToken(provider)
	if err == nil && cached.AccessToken != "" && time.Now().Before(cached.Expiry) {
		slog.Debug("using cached OAuth token", "provider", provider.Name, "expires", cached.Expiry.Format(time.RFC3339))
		return cached.AccessToken, nil
	}
	return runBrowserAuthFlow(ctx, provider, insecure)
}

// InvalidateCache removes the cached token file for the given provider.
func InvalidateCache(provider config.ProviderConfig) {
	path := cacheFilePath(provider)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not remove token cache", "path", path, "err", err)
	}
}

func runBrowserAuthFlow(ctx context.Context, provider config.ProviderConfig, insecure bool) (string, error) {
	pkce, err := newPKCE()
	if err != nil {
		return "", fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := randomURLSafe(16)
	if err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("binding callback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	params := url.Values{
		"client_id":             {provider.Auth.ClientID},
		"response_type":         {"code"},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
	}
	if idp := strings.TrimSpace(provider.Auth.IDP); idp != "" {
		params.Set("idp", idp)
	}
	authURL := strings.TrimSpace(provider.Auth.AuthorizeURL) + "?" + params.Encode()

	slog.Debug("opening browser for OAuth login", "provider", provider.Name)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser. Visit:\n  %s\n", authURL)
	}

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

	accessToken, err := exchangeCodeForToken(ctx, provider, result.code, redirectURI, pkce.Verifier, insecure)
	if err != nil {
		return "", err
	}

	cache := &tokenCache{
		AccessToken: accessToken,
		Expiry:      time.Now().Add(23 * time.Hour),
	}
	if err := saveCachedToken(provider, cache); err != nil {
		slog.Warn("could not cache token", "err", err)
	}

	slog.Debug("OAuth token obtained", "provider", provider.Name)
	return accessToken, nil
}

func exchangeCodeForToken(ctx context.Context, provider config.ProviderConfig, code, redirectURI, codeVerifier string, insecure bool) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}

	if provider.Auth.TokenAuthMethod != config.TokenAuthMethodBasic {
		form.Set("client_id", provider.Auth.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.Auth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	if provider.Auth.TokenAuthMethod == config.TokenAuthMethodBasic {
		req.SetBasicAuth(provider.Auth.ClientID, "")
	}

	client := NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("token exchange returned %s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", errors.New("token exchange returned no access_token")
	}

	return tokenResp.AccessToken, nil
}
