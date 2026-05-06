package snowflake

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/localstack/lstk/internal/emulator"
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Transport: otelhttp.NewTransport(
				http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return "snowflake " + r.Method + " " + r.URL.Path
				}),
			),
		},
	}
}

type healthResponse struct {
	Version string `json:"version"`
}

func (c *Client) FetchVersion(ctx context.Context, host string) (string, error) {
	url := fmt.Sprintf("http://%s/_localstack/health", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create health request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("health endpoint returned status %d", resp.StatusCode)
	}

	var h healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return "", fmt.Errorf("failed to decode health response: %w", err)
	}
	return h.Version, nil
}

// FetchResources is a no-op for Snowflake — the emulator does not expose AWS-style resources.
func (c *Client) FetchResources(_ context.Context, _ string) ([]emulator.Resource, error) {
	return nil, nil
}
