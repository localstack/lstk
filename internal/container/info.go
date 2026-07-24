package container

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/localstack/lstk/internal/telemetry"
)

// ProbeEmulatorInfo fetches /_localstack/info from host ("host:port", plain
// HTTP, 2s timeout). It errors when nothing LocalStack-like answers there:
// transport error, non-200, non-JSON, or a response without a version (any
// JSON object decodes into LocalStackInfo, so an unrelated service returning
// 200 JSON must not count as a LocalStack instance).
func ProbeEmulatorInfo(ctx context.Context, host string) (*telemetry.LocalStackInfo, error) {
	url := fmt.Sprintf("http://%s/_localstack/info", host)
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var info telemetry.LocalStackInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	if info.Version == "" {
		return nil, fmt.Errorf("no LocalStack version in /_localstack/info response")
	}
	return &info, nil
}

func fetchLocalStackInfo(ctx context.Context, port string) (*telemetry.LocalStackInfo, error) {
	return ProbeEmulatorInfo(ctx, "localhost:"+port)
}
