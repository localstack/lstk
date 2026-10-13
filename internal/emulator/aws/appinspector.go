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

// appInspectorStatusPath turns App Inspector on or off. The state resets on
// restart, where the APP_INSPECTOR env var decides.
const appInspectorStatusPath = "/_localstack/appinspector/status"

type appInspectorStatus struct {
	Status  string `json:"status"`
	Changed bool   `json:"changed"`
}

func (c *Client) AppInspectorEnabled(ctx context.Context, baseURL string) (bool, error) {
	s, err := c.appInspectorStatus(ctx, http.MethodGet, baseURL, "")
	return strings.EqualFold(s.Status, "enabled"), err
}

func (c *Client) SetAppInspectorEnabled(ctx context.Context, baseURL string, enabled bool) (bool, error) {
	body := `{"status": "disabled"}`
	if enabled {
		body = `{"status": "enabled"}`
	}
	s, err := c.appInspectorStatus(ctx, http.MethodPut, baseURL, body)
	return s.Changed, err
}

func (c *Client) appInspectorStatus(ctx context.Context, method, baseURL, body string) (appInspectorStatus, error) {
	url := strings.TrimRight(baseURL, "/") + appInspectorStatusPath
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to create App Inspector request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to reach App Inspector: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A 404 means the emulator predates App Inspector, or is the community image.
	if resp.StatusCode == http.StatusNotFound {
		return appInspectorStatus{}, inspect.ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return appInspectorStatus{}, emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), data)
	}
	var s appInspectorStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to decode App Inspector response: %w", err)
	}
	return s, nil
}
