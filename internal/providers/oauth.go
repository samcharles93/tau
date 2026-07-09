package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	githubCopilotID = "github-copilot"
	openAICodexID   = "openai-codex"

	githubDeviceCodeURL   = "https://github.com/login/device/code"
	githubAccessTokenURL  = "https://github.com/login/oauth/access_token"
	githubCopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	githubCopilotClientID = "Iv1.b507a08c87ecfe98"

	// OpenAI Codex does not implement RFC 8628 device authorization (there is
	// no /oauth/device/code grant for this client). ChatGPT sign-in uses a
	// proprietary three-step flow instead: request a user code, poll for an
	// authorization code, then exchange that code via a normal PKCE
	// authorization_code grant. client_id is the Codex CLI's own public
	// client ID; it is not configurable and must match exactly or the auth
	// server rejects the request outright.
	openAICodexClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAICodexVerificationURL = "https://auth.openai.com/codex/device"
	openAICodexRedirectURI     = "https://auth.openai.com/deviceauth/callback"
	openAICodexDeviceTimeout   = 15 * time.Minute
)

// Network endpoints are vars, not consts, so tests can point them at a local
// httptest server instead of the real OpenAI/GitHub auth services.
var (
	oauthHTTPClient = &http.Client{Timeout: 30 * time.Second}

	openAITokenURL            = "https://auth.openai.com/oauth/token"
	openAICodexUserCodeURL    = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	openAICodexDeviceTokenURL = "https://auth.openai.com/api/accounts/deviceauth/token"
)

// DeviceCode holds the user-facing part of an OAuth device-code flow.
type DeviceCode struct {
	Provider        string
	VerificationURI string
	UserCode        string
	ExpiresIn       int
	Interval        int
}

// LoginOptions configures an OAuth login flow.
type LoginOptions struct {
	// EnterpriseDomain is currently used by GitHub Copilot. Empty means
	// github.com. The value is stored for future token refreshes and display.
	EnterpriseDomain string
	// OnDeviceCode is called once a provider returns the verification URL/code.
	OnDeviceCode func(DeviceCode)
	// OnStatus receives non-secret progress messages.
	OnStatus func(string)
}

// OAuthLoginSession is an in-progress device-code login. Callers can display
// DeviceCode immediately, then run Poll to wait for browser authorization.
type OAuthLoginSession struct {
	DeviceCode DeviceCode
	Poll       func(context.Context) (OAuthCredentials, error)
}

// LoginOAuth runs the configured OAuth login flow for a catalog provider and
// returns credentials suitable for State.SetOAuth.
func LoginOAuth(ctx context.Context, providerID string, opts LoginOptions) (OAuthCredentials, error) {
	session, err := BeginOAuthLogin(ctx, providerID, opts)
	if err != nil {
		return OAuthCredentials{}, err
	}
	if opts.OnDeviceCode != nil {
		opts.OnDeviceCode(session.DeviceCode)
	}
	return session.Poll(ctx)
}

// BeginOAuthLogin starts an OAuth device-code flow without polling. This lets
// TUIs render the verification URL/code before running the blocking poll step.
func BeginOAuthLogin(ctx context.Context, providerID string, opts LoginOptions) (OAuthLoginSession, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case githubCopilotID:
		return beginGitHubCopilotLogin(ctx, opts)
	case openAICodexID:
		return beginOpenAICodexLogin(ctx, opts)
	default:
		return OAuthLoginSession{}, fmt.Errorf("provider %q has no OAuth login handler", providerID)
	}
}

// RefreshOAuth refreshes or derives OAuth-backed credentials for a catalog
// provider. It never returns stale access tokens on failure.
func RefreshOAuth(ctx context.Context, providerID string, creds OAuthCredentials) (OAuthCredentials, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case githubCopilotID:
		return refreshGitHubCopilot(ctx, creds)
	case openAICodexID:
		return refreshOpenAICodex(ctx, creds)
	default:
		return OAuthCredentials{}, fmt.Errorf("provider %q has no OAuth refresh handler", providerID)
	}
}

func beginGitHubCopilotLogin(ctx context.Context, opts LoginOptions) (OAuthLoginSession, error) {
	domain := normalizeEnterpriseDomain(opts.EnterpriseDomain)
	device, err := requestForm[githubDeviceResponse](ctx, githubDeviceCodeURL, url.Values{
		"client_id": {githubCopilotClientID},
		"scope":     {"read:user"},
	})
	if err != nil {
		return OAuthLoginSession{}, fmt.Errorf("request GitHub device code: %w", err)
	}
	code := DeviceCode{
		Provider:        githubCopilotID,
		VerificationURI: device.VerificationURI,
		UserCode:        device.UserCode,
		ExpiresIn:       device.ExpiresIn,
		Interval:        device.Interval,
	}
	return OAuthLoginSession{
		DeviceCode: code,
		Poll: func(ctx context.Context) (OAuthCredentials, error) {
			githubToken, err := pollDeviceToken(ctx, githubAccessTokenURL, url.Values{
				"client_id":   {githubCopilotClientID},
				"device_code": {device.DeviceCode},
				"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			}, device.Interval, device.ExpiresIn, opts.OnStatus)
			if err != nil {
				return OAuthCredentials{}, fmt.Errorf("authorize GitHub device login: %w", err)
			}
			creds := OAuthCredentials{
				Refresh: githubToken.AccessToken,
				Extra: map[string]string{
					"enterprise_domain": domain,
				},
			}
			return refreshGitHubCopilot(ctx, creds)
		},
	}, nil
}

func refreshGitHubCopilot(ctx context.Context, creds OAuthCredentials) (OAuthCredentials, error) {
	githubToken := strings.TrimSpace(creds.Refresh)
	if githubToken == "" {
		return OAuthCredentials{}, errors.New("missing GitHub OAuth token; run /provider login github-copilot")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubCopilotTokenURL, nil)
	if err != nil {
		return OAuthCredentials{}, err
	}
	req.Header.Set("Authorization", "Bearer "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tau")
	req.Header.Set("Editor-Version", "tau/1.0")
	req.Header.Set("Editor-Plugin-Version", "tau/1.0")
	req.Header.Set("Copilot-Integration-Id", "tau")

	var payload copilotTokenResponse
	if err := doJSON(req, &payload); err != nil {
		return OAuthCredentials{}, friendlyGitHubCopilotExchangeError(err)
	}
	access := strings.TrimSpace(firstNonEmpty(payload.Token, payload.AccessToken))
	if access == "" {
		return OAuthCredentials{}, errors.New("Copilot token response did not include an access token")
	}
	extra := cloneStringMap(creds.Extra)
	if extra == nil {
		extra = map[string]string{}
	}
	if baseURL := copilotBaseURL(payload); baseURL != "" {
		extra["base_url"] = baseURL
	}
	if len(payload.AvailableModelIDs) > 0 {
		extra["available_model_ids"] = strings.Join(cleanStrings(payload.AvailableModelIDs), ",")
	}
	if domain := normalizeEnterpriseDomain(extra["enterprise_domain"]); domain != "" {
		extra["enterprise_domain"] = domain
	}
	return OAuthCredentials{
		Access:  access,
		Refresh: githubToken,
		Expires: copilotExpiry(payload),
		Extra:   extra,
	}, nil
}

func beginOpenAICodexLogin(ctx context.Context, opts LoginOptions) (OAuthLoginSession, error) {
	device, err := postJSON[codexUserCodeResponse](ctx, openAICodexUserCodeURL, codexUserCodeRequest{ClientID: openAICodexClientID})
	if err != nil {
		return OAuthLoginSession{}, friendlyOpenAIDeviceCodeError(err)
	}
	if strings.TrimSpace(device.UserCode) == "" || strings.TrimSpace(device.DeviceAuthID) == "" {
		return OAuthLoginSession{}, errors.New("OpenAI Codex device code response was missing a device_auth_id or user_code")
	}
	code := DeviceCode{
		Provider:        openAICodexID,
		VerificationURI: openAICodexVerificationURL,
		UserCode:        device.UserCode,
		ExpiresIn:       int(openAICodexDeviceTimeout.Seconds()),
		Interval:        int(device.Interval),
	}
	return OAuthLoginSession{
		DeviceCode: code,
		Poll: func(ctx context.Context) (OAuthCredentials, error) {
			deviceToken, err := pollCodexDeviceToken(ctx, device.DeviceAuthID, device.UserCode, int(device.Interval), opts.OnStatus)
			if err != nil {
				return OAuthCredentials{}, fmt.Errorf("authorize OpenAI Codex device login: %w", err)
			}
			token, err := requestForm[oauthTokenResponse](ctx, openAITokenURL, url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {deviceToken.AuthorizationCode},
				"redirect_uri":  {openAICodexRedirectURI},
				"client_id":     {openAICodexClientID},
				"code_verifier": {deviceToken.CodeVerifier},
			})
			if err != nil {
				return OAuthCredentials{}, fmt.Errorf("exchange OpenAI Codex device code: %w", err)
			}
			return openAITokenCredentials(token), nil
		},
	}, nil
}

func refreshOpenAICodex(ctx context.Context, creds OAuthCredentials) (OAuthCredentials, error) {
	refresh := strings.TrimSpace(creds.Refresh)
	if refresh == "" {
		return OAuthCredentials{}, errors.New("missing OpenAI refresh token; run /provider login openai-codex")
	}
	// Unlike the authorization_code exchange, Codex's refresh grant is JSON,
	// not form-encoded.
	token, err := postJSON[oauthTokenResponse](ctx, openAITokenURL, map[string]string{
		"client_id":     openAICodexClientID,
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
	})
	if err != nil {
		return OAuthCredentials{}, fmt.Errorf("refresh OpenAI Codex token: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return OAuthCredentials{}, errors.New("OpenAI Codex refresh response did not include an access token")
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		token.RefreshToken = refresh
	}
	return openAITokenCredentials(token), nil
}

// openAITokenCredentials builds credentials from a Codex token response.
// Codex's token responses never carry expires_in/expires_at (unlike GitHub's),
// so expiry is read from the exp claim of whichever JWT is present, and the
// ChatGPT account ID comes from the id_token's chatgpt_account_id claim
// (Codex does not put it on the access token).
func openAITokenCredentials(token oauthTokenResponse) OAuthCredentials {
	extra := map[string]string{}
	if accountID := accountIDFromJWT(firstNonEmpty(token.IDToken, token.AccessToken)); accountID != "" {
		extra["account_id"] = accountID
	}
	expires := tokenExpiry(token)
	if expires == 0 {
		expires = jwtExpiry(token.IDToken, token.AccessToken)
	}
	return OAuthCredentials{
		Access:  token.AccessToken,
		Refresh: token.RefreshToken,
		Expires: expires,
		Extra:   extra,
	}
}

// pollCodexDeviceToken polls Codex's device-token endpoint until the user
// completes browser authorization. Unlike the standard RFC 8628 poll (which
// signals "still waiting" via a JSON {"error":"authorization_pending"} body),
// Codex signals it with a bare HTTP 403/404 — any other status is a real
// failure.
func pollCodexDeviceToken(ctx context.Context, deviceAuthID, userCode string, intervalSeconds int, onStatus func(string)) (codexDeviceTokenResponse, error) {
	if intervalSeconds <= 0 {
		intervalSeconds = 5
	}
	deadline := time.Now().Add(openAICodexDeviceTimeout)
	for {
		if time.Now().After(deadline) {
			return codexDeviceTokenResponse{}, errors.New("device code expired")
		}
		select {
		case <-ctx.Done():
			return codexDeviceTokenResponse{}, ctx.Err()
		case <-time.After(time.Duration(intervalSeconds) * time.Second):
		}

		resp, err := postJSON[codexDeviceTokenResponse](ctx, openAICodexDeviceTokenURL, codexDeviceTokenRequest{
			DeviceAuthID: deviceAuthID,
			UserCode:     userCode,
		})
		if err == nil {
			if strings.TrimSpace(resp.AuthorizationCode) == "" {
				return codexDeviceTokenResponse{}, errors.New("device token response did not include an authorization code")
			}
			return resp, nil
		}
		if hasHTTPStatus(err, http.StatusForbidden, http.StatusNotFound) {
			if onStatus != nil {
				onStatus("waiting for browser authorization")
			}
			continue
		}
		return codexDeviceTokenResponse{}, err
	}
}

func pollDeviceToken(
	ctx context.Context,
	endpoint string,
	values url.Values,
	intervalSeconds int,
	expiresIn int,
	onStatus func(string),
) (oauthTokenResponse, error) {
	if intervalSeconds <= 0 {
		intervalSeconds = 5
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for {
		if expiresIn > 0 && time.Now().After(deadline) {
			return oauthTokenResponse{}, errors.New("device code expired")
		}
		select {
		case <-ctx.Done():
			return oauthTokenResponse{}, ctx.Err()
		case <-time.After(time.Duration(intervalSeconds) * time.Second):
		}

		token, err := requestForm[oauthTokenResponse](ctx, endpoint, values)
		if err == nil {
			if strings.TrimSpace(token.AccessToken) == "" {
				return oauthTokenResponse{}, errors.New("token response did not include an access token")
			}
			return token, nil
		}
		var oauthErr oauthError
		if !errors.As(err, &oauthErr) {
			return oauthTokenResponse{}, err
		}
		switch oauthErr.Code {
		case "authorization_pending":
			if onStatus != nil {
				onStatus("waiting for browser authorization")
			}
		case "slow_down":
			intervalSeconds += 5
			if onStatus != nil {
				onStatus("authorization pending; slowing polling")
			}
		case "expired_token":
			return oauthTokenResponse{}, errors.New("device code expired")
		case "access_denied":
			return oauthTokenResponse{}, errors.New("device login denied")
		default:
			return oauthTokenResponse{}, oauthErr
		}
	}
}

// postJSON POSTs payload as a JSON body and decodes a JSON response, for the
// Codex endpoints that don't speak form-encoding.
func postJSON[T any](ctx context.Context, endpoint string, payload any) (T, error) {
	var zero T
	buf, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tau")
	if err := doJSON(req, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func requestForm[T any](ctx context.Context, endpoint string, values url.Values) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "tau")
	if err := doJSON(req, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func doJSON(req *http.Request, out any) error {
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var oe oauthError
		if json.Unmarshal(data, &oe) == nil && oe.Code != "" {
			oe.StatusCode = resp.StatusCode
			return oe
		}
		return httpStatusError{StatusCode: resp.StatusCode, Body: responseSnippet(data)}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty response")
	}
	var oe oauthError
	if json.Unmarshal(data, &oe) == nil && oe.Code != "" {
		return oe
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	StatusCode  int    `json:"-"`
}

func (e oauthError) Error() string {
	var b strings.Builder
	if e.Code != "" {
		b.WriteString(e.Code)
	}
	if e.Description != "" {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(e.Description)
	}
	if b.Len() == 0 {
		b.WriteString("OAuth request failed")
	}
	if e.StatusCode > 0 {
		b.WriteString(fmt.Sprintf(" (HTTP %d)", e.StatusCode))
	}
	return b.String()
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e httpStatusError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

func friendlyOpenAIDeviceCodeError(err error) error {
	if hasHTTPStatus(err, http.StatusNotFound) {
		return errors.New("OpenAI Codex device-code login isn't enabled for this account or workspace. Enable it in ChatGPT security settings (or ask a workspace admin), or use the official Codex CLI login for now. No credentials were saved")
	}
	if hasHTTPStatus(err, http.StatusForbidden) {
		return errors.New("OpenAI rejected Tau's Codex device-code login start request. Tau cannot complete this login flow right now. No credentials were saved. Use the official Codex CLI login for now")
	}
	return fmt.Errorf("request OpenAI device code: %w", err)
}

func friendlyGitHubCopilotExchangeError(err error) error {
	if hasHTTPStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return errors.New("GitHub accepted the login. Copilot rejected API access. Check this GitHub account has an active Copilot subscription. Check organization or enterprise policy allows Copilot API access. No credentials were saved")
	}
	return fmt.Errorf("exchange GitHub token for Copilot token: %w", err)
}

func hasHTTPStatus(err error, statuses ...int) bool {
	var status int
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		status = statusErr.StatusCode
	} else {
		var oauthErr oauthError
		if errors.As(err, &oauthErr) {
			status = oauthErr.StatusCode
		}
	}
	if status == 0 {
		return false
	}
	return slices.Contains(statuses, status)
}

func responseSnippet(data []byte) string {
	snippet := strings.TrimSpace(string(bytes.TrimSpace(data)))
	if snippet == "" {
		return ""
	}
	snippet = strings.Join(strings.Fields(snippet), " ")
	const maxSnippetBytes = 300
	if len(snippet) > maxSnippetBytes {
		snippet = snippet[:maxSnippetBytes] + "..."
	}
	return snippet
}

type githubDeviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// codexUserCodeRequest/Response are step 1 of Codex's device-auth flow:
// requesting the user-facing code. See the const block above for why this
// isn't the standard RFC 8628 shape.
type codexUserCodeRequest struct {
	ClientID string `json:"client_id"`
}

type codexUserCodeResponse struct {
	DeviceAuthID string           `json:"device_auth_id"`
	UserCode     string           `json:"user_code"`
	Interval     codexFlexibleInt `json:"interval"`
}

// codexDeviceTokenRequest/Response are step 2: polling for the browser
// authorization to complete. Success returns a short-lived authorization
// code plus the PKCE verifier/challenge Codex generated server-side, which
// step 3 (a normal authorization_code exchange) consumes as-is.
type codexDeviceTokenRequest struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
}

type codexDeviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeChallenge     string `json:"code_challenge"`
	CodeVerifier      string `json:"code_verifier"`
}

// codexFlexibleInt decodes an interval field that Codex's server sends as
// either a JSON number or a numeric string.
type codexFlexibleInt int

func (f *codexFlexibleInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("decode interval %q: %w", s, err)
	}
	*f = codexFlexibleInt(n)
	return nil
}

type copilotTokenResponse struct {
	Token             string   `json:"token"`
	AccessToken       string   `json:"access_token"`
	ExpiresAt         int64    `json:"expires_at"`
	RefreshIn         int64    `json:"refresh_in"`
	AvailableModelIDs []string `json:"available_model_ids"`
	Endpoints         struct {
		API string `json:"api"`
	} `json:"endpoints"`
	BaseURL string `json:"base_url"`
}

func tokenExpiry(token oauthTokenResponse) int64 {
	if token.ExpiresAt > 0 {
		return token.ExpiresAt
	}
	if token.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
	}
	return 0
}

func copilotExpiry(token copilotTokenResponse) int64 {
	if token.ExpiresAt > 0 {
		return token.ExpiresAt
	}
	if token.RefreshIn > 0 {
		return time.Now().Add(time.Duration(token.RefreshIn) * time.Second).Unix()
	}
	return 0
}

func copilotBaseURL(token copilotTokenResponse) string {
	base := firstNonEmpty(token.BaseURL, token.Endpoints.API)
	return strings.TrimRight(base, "/")
}

func normalizeEnterpriseDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Trim(domain, "/")
	if domain == "" {
		return "github.com"
	}
	return domain
}

func cleanStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func accountIDFromJWT(token string) string {
	claims := jwtClaims(token)
	// chatgpt_account_id is what Codex's id_token actually carries; the rest
	// are defensive fallbacks for other OpenAI JWTs.
	for _, key := range []string{"chatgpt_account_id", "https://api.openai.com/auth/account_id", "account_id", "org_id"} {
		switch v := claims[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

// jwtExpiry returns the exp claim (Unix seconds) of the first decodable JWT
// among tokens, or 0 if none carry one. Codex's token responses never
// include expires_in/expires_at, so this is the only source of expiry.
func jwtExpiry(tokens ...string) int64 {
	for _, token := range tokens {
		if exp, ok := jwtClaims(token)["exp"].(float64); ok && exp > 0 {
			return int64(exp)
		}
	}
	return 0
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
