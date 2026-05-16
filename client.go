// Package doclingclient is a small, idiomatic client for a docling-serve
// instance. It targets the core conversion endpoints (/v1/convert/source and
// /v1/convert/file) and a few status routes.
package doclingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the default docling-serve endpoint when running locally.
const DefaultBaseURL = "http://localhost:5001"

// Client talks to a docling-serve instance. The zero value is not usable;
// construct one with New.
type Client struct {
	// BaseURL is the docling-serve root, e.g. "http://localhost:5001".
	BaseURL string
	// HTTPClient is used for all requests. Defaults to a client with a 5 minute
	// timeout, since document conversion can take a while.
	HTTPClient *http.Client
	// APIKey, when set, is sent as the X-Api-Key header.
	APIKey string
	// TenantID, when set, is sent as the X-Tenant-Id header.
	TenantID string
	// UserAgent overrides the default user agent string.
	UserAgent string
}

// Option configures a Client at construction time. Pass options to New.
type Option func(*Client)

// WithAPIKey sets the X-Api-Key header sent on every request.
func WithAPIKey(key string) Option { return func(c *Client) { c.APIKey = key } }

// WithTenantID sets the X-Tenant-Id header sent on every request.
func WithTenantID(id string) Option { return func(c *Client) { c.TenantID = id } }

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.UserAgent = ua } }

// WithHTTPClient replaces the http.Client used for all requests. Useful for
// tests, custom transports, or a non-default timeout.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.HTTPClient = h } }

// WithTimeout sets a request timeout on the default http.Client. Has no
// effect if a custom client is provided via WithHTTPClient.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.HTTPClient != nil {
			c.HTTPClient.Timeout = d
		}
	}
}

// New returns a Client targeting baseURL. An empty baseURL falls back to
// DefaultBaseURL. Pass options to set credentials, a custom HTTP client, etc.
func New(baseURL string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		UserAgent:  "doclingclient-go",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is returned for non-2xx responses from the docling-serve API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("docling: HTTP %d: %s", e.Status, truncate(e.Body, 512))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// newRequest builds an http.Request relative to BaseURL and applies common
// headers. body may be nil.
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("X-Api-Key", c.APIKey)
	}
	if c.TenantID != "" {
		req.Header.Set("X-Tenant-Id", c.TenantID)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// doJSON sends req and decodes a JSON response into out. If out is nil the
// body is discarded. Non-2xx responses are returned as *APIError.
func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: string(b)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// postJSON marshals v as JSON and POSTs it to path.
func (c *Client) postJSON(ctx context.Context, path string, v, out any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, out)
}
