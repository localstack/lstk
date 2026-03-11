package aws

import (
	"context"
	"net/http"

	"github.com/localstack/lstk/internal/output"
)

// Client defines the interface for communicating with a running AWS emulator instance.
type Client interface {
	FetchVersion(ctx context.Context, host string) (string, error)
	FetchResources(ctx context.Context, host string) ([]output.ResourceRow, error)
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &client{http: httpClient}
}
