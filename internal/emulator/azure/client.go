package azure

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
					return "azure " + r.Method + " " + r.URL.Path
				}),
			),
		},
	}
}

type infoResponse struct {
	Version string `json:"version"`
}

// FetchVersion reads the version from /_localstack/info. The Azure image's
// /_localstack/health response does not carry a "version" field, so /info is
// the only endpoint that surfaces it.
func (c *Client) FetchVersion(ctx context.Context, host string) (string, error) {
	url := fmt.Sprintf("http://%s/_localstack/info", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create info request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("info endpoint returned status %d", resp.StatusCode)
	}

	var i infoResponse
	if err := json.NewDecoder(resp.Body).Decode(&i); err != nil {
		return "", fmt.Errorf("failed to decode info response: %w", err)
	}
	return i.Version, nil
}

// FetchResources is a no-op for Azure — the emulator does not expose
// /_localstack/resources (returns 404).
func (c *Client) FetchResources(_ context.Context, _ string) ([]emulator.Resource, error) {
	return nil, nil
}
