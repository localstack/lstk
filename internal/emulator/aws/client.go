package aws

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

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
					return "aws " + r.Method + " " + r.URL.Path
				}),
			),
		},
	}
}

type healthResponse struct {
	Version string `json:"version"`
}

type instanceResource struct {
	RegionName string `json:"region_name"`
	AccountID  string `json:"account_id"`
	ID         string `json:"id"`
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

func (c *Client) FetchResources(ctx context.Context, host string) ([]emulator.Resource, error) {
	url := fmt.Sprintf("http://%s/_localstack/resources", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resources request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch resources: status %d", resp.StatusCode)
	}

	// Each line of the NDJSON stream is a JSON object mapping an AWS resource type
	// (e.g. "AWS::S3::Bucket") to a list of resource entries.
	var rows []emulator.Resource
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var chunk map[string][]instanceResource
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return nil, fmt.Errorf("failed to parse resource line: %w", err)
		}

		for resourceType, entries := range chunk {
			parts := strings.SplitN(resourceType, "::", 3)
			service := resourceType
			if len(parts) == 3 {
				service = parts[1]
			}

			for _, e := range entries {
				rows = append(rows, emulator.Resource{
					Service: service,
					Name:    extractResourceName(e.ID),
					Region:  e.RegionName,
					Account: e.AccountID,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read resources stream: %w", err)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Service != rows[j].Service {
			return rows[i].Service < rows[j].Service
		}
		return rows[i].Name < rows[j].Name
	})

	return rows, nil
}
