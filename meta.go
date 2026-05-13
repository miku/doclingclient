package doclingclient

import (
	"context"
	"net/http"
)

// HealthResponse is returned by /health and /ready.
type HealthResponse struct {
	Status string `json:"status"`
}

// Health calls GET /health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	return c.getHealth(ctx, "/health")
}

// Ready calls GET /ready.
func (c *Client) Ready(ctx context.Context) (*HealthResponse, error) {
	return c.getHealth(ctx, "/ready")
}

func (c *Client) getHealth(ctx context.Context, path string) (*HealthResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out HealthResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Version calls GET /version and returns the raw key/value map (the schema is
// open-ended).
func (c *Client) Version(ctx context.Context) (map[string]any, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/version", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return out, nil
}
