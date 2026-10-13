package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/localstack/lstk/internal/inspect"
)

// appInspectorStatusPath is App Inspector's runtime switch. The emulator does
// not keep the state across restarts; there APP_INSPECTOR decides.
const appInspectorStatusPath = "/_localstack/appinspector/status"

type appInspectorStatus struct {
	Status  string `json:"status"`
	Changed bool   `json:"changed"`
}

func (c *Client) AppInspectorEnabled(ctx context.Context, baseURL string) (bool, error) {
	s, err := c.appInspectorStatus(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return false, err
	}
	return s.enabled()
}

func (c *Client) SetAppInspectorEnabled(ctx context.Context, baseURL string, enabled bool) (bool, error) {
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	body, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return false, err
	}
	s, err := c.appInspectorStatus(ctx, http.MethodPut, baseURL, body)
	if err != nil {
		return false, err
	}
	got, err := s.enabled()
	if err != nil {
		return false, err
	}
	if got != enabled {
		return false, fmt.Errorf("emulator reported App Inspector status %s after the change", s.Status)
	}
	return s.Changed, nil
}

func (c *Client) appInspectorStatus(ctx context.Context, method, baseURL string, body []byte) (appInspectorStatus, error) {
	url := strings.TrimRight(baseURL, "/") + appInspectorStatusPath
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to create App Inspector request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to reach App Inspector: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to read App Inspector response: %w", err)
	}
	// Every licensed emulator that ships App Inspector registers this route, so
	// a 404 means this one predates it (or is the community image).
	if resp.StatusCode == http.StatusNotFound {
		return appInspectorStatus{}, inspect.ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			data = []byte(e.Error)
		}
		return appInspectorStatus{}, emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), data)
	}

	var s appInspectorStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return appInspectorStatus{}, fmt.Errorf("failed to decode App Inspector response: %w", err)
	}
	return s, nil
}

func (s appInspectorStatus) enabled() (bool, error) {
	switch strings.ToUpper(s.Status) {
	case "ENABLED":
		return true, nil
	case "DISABLED":
		return false, nil
	}
	return false, fmt.Errorf("unexpected App Inspector status %q", s.Status)
}
