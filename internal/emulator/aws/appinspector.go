package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/localstack/lstk/internal/inspect"
)

const appInspectorStatusPath = "/_localstack/appinspector/status"

func (c *Client) SetAppInspectorEnabled(ctx context.Context, baseURL string, enabled bool) (bool, error) {
	body := `{"status": "disabled"}`
	if enabled {
		body = `{"status": "enabled"}`
	}
	url := strings.TrimRight(baseURL, "/") + appInspectorStatusPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("failed to create App Inspector request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to reach App Inspector: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The route is missing on older images and the community image.
	if resp.StatusCode == http.StatusNotFound {
		return false, inspect.ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return false, emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), data)
	}
	var s struct {
		Changed bool `json:"changed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return false, fmt.Errorf("failed to decode App Inspector response: %w", err)
	}
	return s.Changed, nil
}
