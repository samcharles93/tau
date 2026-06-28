// Package registry provides an HTTP client for the Tau plugin registry API.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultRegistry is the default plugin registry base URL.
const DefaultRegistry = "https://registry.tau-ai.dev"

// Client is an HTTP client for the Tau plugin registry REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a registry API client. baseURL should be the registry
// root, e.g. "https://registry.tau-ai.dev". token is optional (only needed
// for write operations like publish).
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ---------- response types (mirror the API contract) ----------

// ExtensionSummary is the lightweight list/search view of an extension.
type ExtensionSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Verified    bool     `json:"verified"`
	Downloads   int64    `json:"downloads"`
	Latest      string   `json:"latest_version"`
	UpdatedAt   string   `json:"updated_at"`
}

// ExtensionDetail is the full extension record returned by GET /extensions/:id.
type ExtensionDetail struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Author        string   `json:"author"`
	Repo          string   `json:"repo"`
	Category      string   `json:"category"`
	License       string   `json:"license"`
	Homepage      string   `json:"homepage"`
	Tags          []string `json:"tags"`
	Verified      bool     `json:"verified"`
	Downloads     int64    `json:"downloads"`
	MinTauVersion string   `json:"min_tau_version"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// VersionInfo is a lightweight version entry (from list endpoints).
type VersionInfo struct {
	Version     string `json:"version"`
	Size        int64  `json:"size"`
	GoVersion   string `json:"go_version"`
	Checksum    string `json:"checksum"`
	ReleaseURL  string `json:"release_url"`
	PublishedAt string `json:"published_at"`
}

// VersionDetail is the full version record including platform binaries.
type VersionDetail struct {
	VersionInfo
	ReleaseNotes  string     `json:"release_notes"`
	MinTauVersion string     `json:"min_tau_version"`
	Platforms     []Platform `json:"platforms"`
}

// Platform is an OS/arch-specific binary download.
type Platform struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
}

// SearchResult wraps a paginated search response.
type SearchResult struct {
	Data    []ExtensionSummary `json:"data"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	PerPage int                `json:"per_page"`
}

// TagCount is a tag with its usage count.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Stats holds registry aggregate statistics.
type Stats struct {
	Extensions int64 `json:"extensions"`
	Versions   int64 `json:"versions"`
	Downloads  int64 `json:"downloads"`
}

// PublishRequest is the body for POST /extensions.
type PublishRequest struct {
	ID            string   `json:"id,omitempty"` // explicit extension ID; if empty, derived from repo
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Author        string   `json:"author"`
	Repo          string   `json:"repo"`
	Category      string   `json:"category"`
	License       string   `json:"license"`
	Homepage      string   `json:"homepage,omitempty"`
	Tags          []string `json:"tags"`
	MinTauVersion string   `json:"min_tau_version,omitempty"`
}

// CreateVersionRequest is the body for POST /extensions/:id/versions.
type CreateVersionRequest struct {
	Version       string     `json:"version"`
	Checksum      string     `json:"checksum"`
	Size          int64      `json:"size"`
	GoVersion     string     `json:"go_version"`
	ReleaseURL    string     `json:"release_url"`
	ReleaseNotes  string     `json:"release_notes"`
	MinTauVersion string     `json:"min_tau_version"`
	Platforms     []Platform `json:"platforms"`
}

// APIError is returned when the registry responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("registry: %s (status %d)", e.Message, e.StatusCode)
}

// ---------- read operations ----------

// Health checks whether the registry is reachable.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, "GET", "/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Stats returns aggregate registry statistics.
func (c *Client) Stats(ctx context.Context) (*Stats, error) {
	var v Stats
	if err := c.getJSON(ctx, "/api/v1/stats", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Search queries the registry for extensions matching the given criteria.
func (c *Client) Search(ctx context.Context, query, category string, tags []string, author, sort string, page, perPage int) (*SearchResult, error) {
	q := url.Values{}
	if query != "" {
		q.Set("q", query)
	}
	if category != "" {
		q.Set("category", category)
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}
	if author != "" {
		q.Set("author", author)
	}
	if sort != "" {
		q.Set("sort", sort)
	}
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	if perPage > 0 {
		q.Set("per_page", fmt.Sprintf("%d", perPage))
	}

	var v SearchResult
	if err := c.getJSON(ctx, "/api/v1/search?"+q.Encode(), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetExtension returns the full extension record.
func (c *Client) GetExtension(ctx context.Context, id string) (*ExtensionDetail, error) {
	var v ExtensionDetail
	if err := c.getJSON(ctx, "/api/v1/extensions/"+url.PathEscape(id), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ListVersions returns all versions for an extension, newest first.
func (c *Client) ListVersions(ctx context.Context, id string) ([]VersionInfo, error) {
	var v []VersionInfo
	if err := c.getJSON(ctx, "/api/v1/extensions/"+url.PathEscape(id)+"/versions", &v); err != nil {
		return nil, err
	}
	return v, nil
}

// GetVersion returns a specific version with its platform binaries.
func (c *Client) GetVersion(ctx context.Context, id, version string) (*VersionDetail, error) {
	var v VersionDetail
	path := fmt.Sprintf("/api/v1/extensions/%s/versions/%s", url.PathEscape(id), url.PathEscape(version))
	if err := c.getJSON(ctx, path, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetDownloadURL returns the redirect URL for downloading a specific platform binary.
func (c *Client) GetDownloadURL(ctx context.Context, id, version, os, arch string) (string, error) {
	path := fmt.Sprintf(
		"/api/v1/extensions/%s/versions/%s/download?os=%s&arch=%s",
		url.PathEscape(id), url.PathEscape(version),
		url.QueryEscape(os), url.QueryEscape(arch),
	)

	resp, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		return "", parseError(resp)
	}
	return resp.Header.Get("Location"), nil
}

// ListTags returns all tags with usage counts.
func (c *Client) ListTags(ctx context.Context) ([]TagCount, error) {
	var v []TagCount
	if err := c.getJSON(ctx, "/api/v1/tags", &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ---------- write operations ----------

// PublishExtension registers a new extension (or updates an existing one the
// caller owns). Requires an API token.
func (c *Client) PublishExtension(ctx context.Context, ext *PublishRequest) error {
	body, err := json.Marshal(ext)
	if err != nil {
		return fmt.Errorf("marshal extension: %w", err)
	}

	resp, err := c.do(ctx, "POST", "/api/v1/extensions", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return parseError(resp)
	}
	return nil
}

// PublishVersion creates a new version for an extension. Requires an API token.
func (c *Client) PublishVersion(ctx context.Context, id string, v *CreateVersionRequest) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal version: %w", err)
	}

	path := fmt.Sprintf("/api/v1/extensions/%s/versions", url.PathEscape(id))
	resp, err := c.do(ctx, "POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return parseError(resp)
	}
	return nil
}

// ---------- internal ----------

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	resp, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &APIError{StatusCode: 404, Message: "not found", Code: "NOT_FOUND"}
	}
	if resp.StatusCode >= 400 {
		return parseError(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	reqURL := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func parseError(resp *http.Response) error {
	var apiErr struct {
		Message string `json:"error"`
		Code    string `json:"code"`
	}
	// Best-effort decode; fall back to status text.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(body, &apiErr)
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    apiErr.Message,
		Code:       apiErr.Code,
	}
}

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
