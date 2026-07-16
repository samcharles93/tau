package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFriendlyOpenAIDeviceCodeErrorFor403(t *testing.T) {
	err := friendlyOpenAIDeviceCodeError(httpStatusError{StatusCode: http.StatusForbidden})
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	for _, want := range []string{
		"OpenAI rejected Tau's Codex device-code login start request",
		"Tau cannot complete this login flow right now",
		"No credentials were saved",
		"official Codex CLI login",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
	if strings.Contains(got, "status 403") {
		t.Fatalf("expected friendly error, got raw status text: %q", got)
	}
}

func TestFriendlyOpenAIDeviceCodeErrorFor404(t *testing.T) {
	err := friendlyOpenAIDeviceCodeError(httpStatusError{StatusCode: http.StatusNotFound})
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	for _, want := range []string{
		"device-code login isn't enabled",
		"No credentials were saved",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
}

func TestFriendlyGitHubCopilotExchangeErrorFor403(t *testing.T) {
	err := friendlyGitHubCopilotExchangeError(oauthError{Code: "forbidden", StatusCode: http.StatusForbidden})
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	for _, want := range []string{
		"GitHub accepted the login",
		"Copilot rejected API access",
		"active Copilot subscription",
		"organization or enterprise policy",
		"No credentials were saved",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
	if strings.Contains(got, "status 403") {
		t.Fatalf("expected friendly error, got raw status text: %q", got)
	}
}

func TestFriendlyErrorsPreserveUnexpectedFailures(t *testing.T) {
	root := errors.New("connection refused")

	openAIErr := friendlyOpenAIDeviceCodeError(root)
	if !errors.Is(openAIErr, root) {
		t.Fatalf("expected OpenAI error to wrap root cause")
	}
	if !strings.Contains(openAIErr.Error(), "request OpenAI device code") {
		t.Fatalf("expected OpenAI context, got %q", openAIErr.Error())
	}

	copilotErr := friendlyGitHubCopilotExchangeError(root)
	if !errors.Is(copilotErr, root) {
		t.Fatalf("expected Copilot error to wrap root cause")
	}
	if !strings.Contains(copilotErr.Error(), "exchange GitHub token for Copilot token") {
		t.Fatalf("expected Copilot context, got %q", copilotErr.Error())
	}
}

func TestResponseSnippetCompactsAndTruncatesBodies(t *testing.T) {
	body := []byte(" \n\t error    details " + strings.Repeat("x", 400))
	got := responseSnippet(body)
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") || strings.Contains(got, "    ") {
		t.Fatalf("expected compact snippet, got %q", got)
	}
	if len(got) > 303 {
		t.Fatalf("expected snippet to be truncated, got length %d", len(got))
	}
}

// TestOpenAICodexLoginEndToEnd exercises Codex's proprietary three-step
// device-auth flow (JSON usercode request -> JSON polling that signals
// "pending" via bare 403s -> form-encoded PKCE authorization_code exchange)
// against a mock auth server, guarding against regressing back to the
// standard RFC 8628 shape OpenAI's server doesn't implement for this client.
func TestOpenAICodexLoginEndToEnd(t *testing.T) {
	var userCodeCalls, deviceTokenCalls, tokenCalls int32
	idToken := fakeJWT(t, map[string]any{
		"chatgpt_account_id": "acct-1",
		"exp":                float64(time.Now().Add(time.Hour).Unix()),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&userCodeCalls, 1)
		var req codexUserCodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode usercode request: %v", err)
		}
		if req.ClientID != openAICodexClientID {
			t.Errorf("client_id = %q, want %q", req.ClientID, openAICodexClientID)
		}
		_ = json.NewEncoder(w).Encode(codexUserCodeResponse{DeviceAuthID: "da-1", UserCode: "ABCD-1234", Interval: 1})
	})
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&deviceTokenCalls, 1)
		var req codexDeviceTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode device token poll request: %v", err)
		}
		if req.DeviceAuthID != "da-1" || req.UserCode != "ABCD-1234" {
			t.Errorf("unexpected poll request: %+v", req)
		}
		if n == 1 {
			// First poll: browser authorization still pending. Codex signals
			// this with a bare 403, not an RFC 8628 error body.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(codexDeviceTokenResponse{
			AuthorizationCode: "code-1",
			CodeChallenge:     "chal-1",
			CodeVerifier:      "verifier-1",
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token exchange form: %v", err)
		}
		want := map[string]string{
			"grant_type":    "authorization_code",
			"code":          "code-1",
			"redirect_uri":  openAICodexRedirectURI,
			"client_id":     openAICodexClientID,
			"code_verifier": "verifier-1",
		}
		for key, wantVal := range want {
			if got := r.FormValue(key); got != wantVal {
				t.Errorf("form[%s] = %q, want %q", key, got, wantVal)
			}
		}
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "access-1", RefreshToken: "refresh-1", IDToken: idToken})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapCodexEndpoints(t, srv.URL)()

	session, err := beginOpenAICodexLogin(context.Background(), LoginOptions{})
	if err != nil {
		t.Fatalf("beginOpenAICodexLogin() error = %v", err)
	}
	if session.DeviceCode.UserCode != "ABCD-1234" {
		t.Fatalf("UserCode = %q, want ABCD-1234", session.DeviceCode.UserCode)
	}
	if session.DeviceCode.VerificationURI != openAICodexVerificationURL {
		t.Fatalf("VerificationURI = %q, want %q", session.DeviceCode.VerificationURI, openAICodexVerificationURL)
	}

	creds, err := session.Poll(context.Background())
	if err != nil {
		t.Fatalf("session.Poll() error = %v", err)
	}
	if creds.Access != "access-1" || creds.Refresh != "refresh-1" {
		t.Fatalf("creds = %+v, want access-1/refresh-1", creds)
	}
	if creds.Extra["account_id"] != "acct-1" {
		t.Fatalf("account_id = %q, want acct-1 (from the id_token's chatgpt_account_id claim)", creds.Extra["account_id"])
	}
	if creds.Expired() {
		t.Fatal("expected Expires to be derived from the id_token's exp claim, not left at 0")
	}
	if got := atomic.LoadInt32(&userCodeCalls); got != 1 {
		t.Fatalf("usercode calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&deviceTokenCalls); got != 2 {
		t.Fatalf("device token poll calls = %d, want 2 (one pending 403, one success)", got)
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 1 {
		t.Fatalf("token exchange calls = %d, want 1", got)
	}
}

func TestRefreshOpenAICodexUsesJSONBody(t *testing.T) {
	var gotContentType string
	var gotBody map[string]string
	idToken := fakeJWT(t, map[string]any{
		"chatgpt_account_id": "acct-2",
		"exp":                float64(time.Now().Add(time.Hour).Unix()),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode refresh request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "access-2", RefreshToken: "refresh-2", IDToken: idToken})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapCodexEndpoints(t, srv.URL)()

	creds, err := refreshOpenAICodex(context.Background(), OAuthCredentials{Refresh: "refresh-1"})
	if err != nil {
		t.Fatalf("refreshOpenAICodex() error = %v", err)
	}
	if creds.Access != "access-2" || creds.Refresh != "refresh-2" {
		t.Fatalf("creds = %+v, want access-2/refresh-2", creds)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["client_id"] != openAICodexClientID || gotBody["grant_type"] != "refresh_token" || gotBody["refresh_token"] != "refresh-1" {
		t.Fatalf("refresh request body = %+v", gotBody)
	}
}

// swapCodexEndpoints points the Codex OAuth endpoints at a local mock
// server for the duration of a test and returns a restore func.
func swapCodexEndpoints(t *testing.T, baseURL string) func() {
	t.Helper()
	origUserCode, origDeviceToken, origToken := openAICodexUserCodeURL, openAICodexDeviceTokenURL, openAITokenURL
	openAICodexUserCodeURL = baseURL + "/api/accounts/deviceauth/usercode"
	openAICodexDeviceTokenURL = baseURL + "/api/accounts/deviceauth/token"
	openAITokenURL = baseURL + "/oauth/token"
	return func() {
		openAICodexUserCodeURL, openAICodexDeviceTokenURL, openAITokenURL = origUserCode, origDeviceToken, origToken
	}
}

func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal fake JWT claims: %v", err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestGithubEndpointsGitHubCom(t *testing.T) {
	device, access, copilot := githubEndpoints("github.com")
	if device != "https://github.com/login/device/code" {
		t.Errorf("device URL = %q", device)
	}
	if access != "https://github.com/login/oauth/access_token" {
		t.Errorf("access token URL = %q", access)
	}
	if copilot != "https://api.github.com/copilot_internal/v2/token" {
		t.Errorf("copilot token URL = %q", copilot)
	}
}

func TestGithubEndpointsEnterpriseCloud(t *testing.T) {
	device, access, copilot := githubEndpoints("contoso.ghe.com")
	if device != "https://contoso.ghe.com/login/device/code" {
		t.Errorf("device URL = %q", device)
	}
	if access != "https://contoso.ghe.com/login/oauth/access_token" {
		t.Errorf("access token URL = %q", access)
	}
	if want := "https://api.contoso.ghe.com/copilot_internal/v2/token"; copilot != want {
		t.Errorf("copilot token URL = %q, want %q", copilot, want)
	}
}

func TestGithubEndpointsEnterpriseServer(t *testing.T) {
	device, access, copilot := githubEndpoints("github.mycompany.com")
	if device != "https://github.mycompany.com/login/device/code" {
		t.Errorf("device URL = %q", device)
	}
	if access != "https://github.mycompany.com/login/oauth/access_token" {
		t.Errorf("access token URL = %q", access)
	}
	if want := "https://github.mycompany.com/api/v3/copilot_internal/v2/token"; copilot != want {
		t.Errorf("copilot token URL = %q, want %q", copilot, want)
	}
}

// TestGitHubCopilotLoginEndToEnd exercises beginGitHubCopilotLogin +
// session.Poll against a local mock server, verifying the enterprise domain
// travels through to every request (device code, access token, and Copilot
// token exchange) rather than silently falling back to github.com - the bug
// this test guards against previously went unnoticed because the endpoint
// consts weren't swappable at all.
func TestGitHubCopilotLoginEndToEnd(t *testing.T) {
	var deviceCodeCalls, tokenCalls, copilotCalls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deviceCodeCalls, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse device code form: %v", err)
		}
		if got := r.FormValue("client_id"); got != githubCopilotClientID {
			t.Errorf("client_id = %q, want %q", got, githubCopilotClientID)
		}
		_ = json.NewEncoder(w).Encode(githubDeviceResponse{
			DeviceCode:      "devcode-1",
			UserCode:        "ABCD-1234",
			VerificationURI: "https://example.test/device",
			ExpiresIn:       900,
			Interval:        1,
		})
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenCalls, 1)
		if n == 1 {
			// First poll: browser authorization still pending, the standard
			// RFC 8628 way (unlike Codex's bespoke bare-403 signal).
			_ = json.NewEncoder(w).Encode(oauthError{Code: "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "gh-access-1"})
	})
	mux.HandleFunc("/copilot_internal/v2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&copilotCalls, 1)
		if got := r.Header.Get("Authorization"); got != "Bearer gh-access-1" {
			t.Errorf("Authorization = %q, want Bearer gh-access-1", got)
		}
		_ = json.NewEncoder(w).Encode(copilotTokenResponse{
			Token:     "copilot-token-1",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapGitHubEndpoints(t, srv.URL)()

	session, err := beginGitHubCopilotLogin(context.Background(), LoginOptions{EnterpriseDomain: "github.mycompany.com"})
	if err != nil {
		t.Fatalf("beginGitHubCopilotLogin() error = %v", err)
	}
	if session.DeviceCode.UserCode != "ABCD-1234" {
		t.Fatalf("UserCode = %q, want ABCD-1234", session.DeviceCode.UserCode)
	}

	creds, err := session.Poll(context.Background())
	if err != nil {
		t.Fatalf("session.Poll() error = %v", err)
	}
	if creds.Access != "copilot-token-1" {
		t.Fatalf("Access = %q, want copilot-token-1", creds.Access)
	}
	if creds.Refresh != "gh-access-1" {
		t.Fatalf("Refresh = %q, want gh-access-1", creds.Refresh)
	}
	if creds.Extra["enterprise_domain"] != "github.mycompany.com" {
		t.Fatalf("enterprise_domain = %q, want github.mycompany.com", creds.Extra["enterprise_domain"])
	}
	if got := atomic.LoadInt32(&deviceCodeCalls); got != 1 {
		t.Fatalf("device code calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 2 {
		t.Fatalf("token poll calls = %d, want 2 (one pending, one success)", got)
	}
	if got := atomic.LoadInt32(&copilotCalls); got != 1 {
		t.Fatalf("copilot token calls = %d, want 1", got)
	}
}

// TestRefreshGitHubCopilotUsesStoredDomain verifies token refresh (not just
// initial login) stays pinned to the host recorded in creds.Extra at login
// time, rather than reverting to github.com.
func TestRefreshGitHubCopilotUsesStoredDomain(t *testing.T) {
	var copilotCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/copilot_internal/v2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&copilotCalls, 1)
		if got := r.Header.Get("Authorization"); got != "Bearer gh-refresh-1" {
			t.Errorf("Authorization = %q, want Bearer gh-refresh-1", got)
		}
		_ = json.NewEncoder(w).Encode(copilotTokenResponse{
			Token:     "copilot-token-2",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer swapGitHubEndpoints(t, srv.URL)()

	creds, err := refreshGitHubCopilot(context.Background(), OAuthCredentials{
		Refresh: "gh-refresh-1",
		Extra:   map[string]string{"enterprise_domain": "github.mycompany.com"},
	})
	if err != nil {
		t.Fatalf("refreshGitHubCopilot() error = %v", err)
	}
	if creds.Access != "copilot-token-2" {
		t.Fatalf("Access = %q, want copilot-token-2", creds.Access)
	}
	if got := atomic.LoadInt32(&copilotCalls); got != 1 {
		t.Fatalf("copilot token calls = %d, want 1", got)
	}
}

// swapGitHubEndpoints points all three GitHub OAuth/Copilot endpoints at a
// local mock server for the duration of a test and returns a restore func -
// mirrors swapCodexEndpoints above. It ignores the domain argument since the
// mock server serves all three paths itself; domain-to-URL mapping is
// covered separately by TestGithubEndpoints*.
func swapGitHubEndpoints(t *testing.T, baseURL string) func() {
	t.Helper()
	orig := githubEndpoints
	githubEndpoints = func(string) (deviceCodeURL, accessTokenURL, copilotTokenURL string) {
		return baseURL + "/login/device/code", baseURL + "/login/oauth/access_token", baseURL + "/copilot_internal/v2/token"
	}
	return func() { githubEndpoints = orig }
}
