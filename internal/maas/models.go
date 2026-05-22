package maas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

// Model represents a model returned by the MaaS API.
type Model struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Ready bool   `json:"ready"`
}

// modelsResponse is the envelope from GET /maas-api/v1/models.
type modelsResponse struct {
	Data []Model `json:"data"`
}

// DiscoverModels fetches the list of available models from the MaaS gateway.
func DiscoverModels(ctx context.Context, endpoint platform.Endpoint, maasToken string, insecure bool) ([]Model, error) {
	url := endpoint.MaaSGateway + "/maas-api/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+maasToken)

	client := platform.NewHTTPClient(insecure)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model discovery failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading model discovery response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("MaaS token is invalid or expired (401). Re-run 'aim token' to obtain a new one")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model discovery returned %d: %s", resp.StatusCode, body)
	}

	var response modelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("invalid JSON from models endpoint: %w", err)
	}
	return response.Data, nil
}
