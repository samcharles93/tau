package maas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

// maasTokenRequest is the JSON body for the MaaS token exchange.
type maasTokenRequest struct {
	Expiration string `json:"expiration"`
}

// maasTokenResponse is the JSON response from the MaaS token endpoint.
type maasTokenResponse struct {
	Token string `json:"token"`
}

// ResolveMaaSToken returns a usable MaaS token, honoring any overrides first.
// If the cached OCP token is rejected (401), it automatically retries with a
// fresh browser auth flow.
func ResolveMaaSToken(ctx context.Context, endpoint platform.Endpoint, insecure bool, maasToken, ocpToken, expiry string) (string, error) {
	maasToken = strings.TrimSpace(maasToken)
	if maasToken != "" {
		return maasToken, nil
	}

	ocpToken = strings.TrimSpace(ocpToken)
	usedCache := false
	var err error
	if ocpToken == "" {
		ocpToken, err = platform.GetOCPToken(ctx, endpoint, insecure)
		if err != nil {
			return "", err
		}
		usedCache = true
	}

	expiry = strings.TrimSpace(expiry)
	if expiry == "" {
		expiry = aimconfig.LoadConfig().TokenExpiry
	}
	if expiry == "" {
		expiry = "8h"
	}

	result, err := exchangeMaaSToken(ctx, endpoint, ocpToken, expiry, insecure)
	if err != nil && usedCache && isUnauthorizedError(err) {
		// Cached token was stale — re-authenticate via browser.
		slog.Debug("cached OCP token rejected, re-authenticating")
		ocpToken, err = platform.GetFreshOCPToken(ctx, endpoint, insecure)
		if err != nil {
			return "", err
		}
		return exchangeMaaSToken(ctx, endpoint, ocpToken, expiry, insecure)
	}

	return result, err
}

// isUnauthorizedError checks if the error is a 401 rejection.
func isUnauthorizedError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "(401)")
}

// exchangeMaaSToken exchanges an OCP token for a MaaS JWT via the gateway API.
func exchangeMaaSToken(ctx context.Context, endpoint platform.Endpoint, ocpToken, expiry string, insecure bool) (string, error) {
	slog.Debug("requesting MaaS token", "expiry", expiry)

	body, err := json.Marshal(maasTokenRequest{Expiration: expiry})
	if err != nil {
		return "", err
	}

	url := endpoint.MaaSGateway + "/maas-api/v1/tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ocpToken)
	req.Header.Set("Content-Type", "application/json")

	client := platform.NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("MaaS token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// success
	case http.StatusUnauthorized:
		return "", fmt.Errorf("OCP token rejected by MaaS gateway (401).\nResponse: %s", respBody)
	case http.StatusForbidden, http.StatusInternalServerError:
		return "", fmt.Errorf("MaaS gateway returned %d — your user may not be entitled to MaaS on this cluster.\n"+
			"Ask a cluster admin to add you to the relevant MaaS access group.\nResponse: %s",
			resp.StatusCode, respBody)
	default:
		return "", fmt.Errorf("MaaS token endpoint returned %d: %s", resp.StatusCode, respBody)
	}

	var tokenResp maasTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("invalid JSON from MaaS token endpoint: %w", err)
	}
	if tokenResp.Token == "" {
		return "", fmt.Errorf("MaaS token endpoint returned empty token. Response: %s", respBody)
	}

	slog.Debug("MaaS token obtained")
	return tokenResp.Token, nil
}
