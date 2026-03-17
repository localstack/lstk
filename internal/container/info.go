package container

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/localstack/lstk/internal/telemetry"
)

func fetchLocalStackInfo(ctx context.Context, port string) (*telemetry.LocalStackInfo, error) {
	url := fmt.Sprintf("http://localhost:%s/_localstack/info", port)
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
	return &info, nil
}
