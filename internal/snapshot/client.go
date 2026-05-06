package snapshot

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// StateExporter retrieves state from the running LocalStack instance.
type StateExporter interface {
	ExportState(ctx context.Context) (io.ReadCloser, error)
}

// StateClient calls the LocalStack state API.
type StateClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewStateClient(baseURL string) *StateClient {
	return &StateClient{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// ExportState calls GET /_localstack/pods/state; caller must close the returned body.
func (c *StateClient) ExportState(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_localstack/pods/state", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("LocalStack returned status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
