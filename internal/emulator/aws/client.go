package aws

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/localstack/lstk/internal/snapshot"
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

func (c *Client) ResetState(ctx context.Context, host string) error {
	url := fmt.Sprintf("http://%s/_localstack/state/reset", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LocalStack returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ExportState(ctx context.Context, host string, dst io.Writer) error {
	url := fmt.Sprintf("http://%s/_localstack/pods/state", host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LocalStack returned status %d", resp.StatusCode)
	}

	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("stream state: %w", err)
	}
	return nil
}

func (c *Client) ImportState(ctx context.Context, host string, src io.Reader, strategy string) error {
	url := fmt.Sprintf("http://%s/_localstack/pods", host)
	if strategy != "" {
		url += "?merge=" + strategy
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, src)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s", snapshot.ErrIncompatibleSnapshot, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LocalStack returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Service string `json:"service"`
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Status == "error" && event.Message != "" {
			if isInvalidSnapshotFileMsg(event.Message) {
				return snapshot.ErrInvalidSnapshotFile
			}
			return fmt.Errorf("load failed for service %s: %s", event.Service, event.Message)
		}
	}
	return scanner.Err()
}

// isInvalidSnapshotFileMsg reports whether an emulator error message indicates
// the source could not be read as a snapshot archive. We translate these into
// snapshot.ErrInvalidSnapshotFile so the user-facing message never leaks the
// underlying archive format.
func isInvalidSnapshotFileMsg(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "not a valid zip archive") || strings.Contains(m, "invalid pod file")
}

// isPodNotFoundMsg reports whether an emulator error message indicates the
// requested cloud snapshot does not exist. The emulator reports an unknown pod
// with this generic version-lookup message rather than a distinct not-found
// error, so we translate it into snapshot.ErrPodNotFound.
func isPodNotFoundMsg(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "failed to get version information from platform")
}

func (c *Client) LoadPodSnapshot(ctx context.Context, host, podName, authToken, strategy string) ([]string, error) {
	url := fmt.Sprintf("http://%s/_localstack/pods/%s", host, podName)
	if strategy != "" {
		url += "?merge=" + strategy
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s", snapshot.ErrIncompatibleSnapshot, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pod load failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var services []string
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Event   string `json:"event"`
			Service string `json:"service"`
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		switch event.Event {
		case "service":
			switch event.Status {
			case "ok":
				services = append(services, event.Service)
			case "error":
				if isInvalidSnapshotFileMsg(event.Message) {
					return nil, snapshot.ErrInvalidSnapshotFile
				}
				return nil, fmt.Errorf("load failed for service %s: %s", event.Service, event.Message)
			}
		case "completion":
			if event.Status != "ok" {
				if isInvalidSnapshotFileMsg(event.Message) {
					return nil, snapshot.ErrInvalidSnapshotFile
				}
				if isPodNotFoundMsg(event.Message) {
					return nil, snapshot.ErrPodNotFound
				}
				return nil, fmt.Errorf("pod load failed: %s", event.Message)
			}
			return services, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return services, nil
}

func (c *Client) SavePodSnapshot(ctx context.Context, host, podName, authToken string) (snapshot.PodSaveResult, error) {
	url := fmt.Sprintf("http://%s/_localstack/pods/%s", host, podName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.http.Do(req)
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return snapshot.PodSaveResult{}, fmt.Errorf("pod save failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// The response is a newline-delimited JSON stream. We scan until we find a
	// completion event and surface any server-side error as a Go error.
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Event   string `json:"event"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Info    struct {
				Version  int      `json:"version"`
				Services []string `json:"services"`
				Size     int64    `json:"size"`
			} `json:"info"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Event == "completion" {
			if event.Status != "ok" {
				return snapshot.PodSaveResult{}, fmt.Errorf("pod save failed: %s", event.Message)
			}
			return snapshot.PodSaveResult{
				Version:  event.Info.Version,
				Services: event.Info.Services,
				Size:     event.Info.Size,
			}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("reading response: %w", err)
	}
	return snapshot.PodSaveResult{}, fmt.Errorf("pod save: server closed stream without a completion event")
}

func (c *Client) RemovePodSnapshot(ctx context.Context, host, podName, authToken string) error {
	url := fmt.Sprintf("http://%s/_localstack/pods/%s", host, podName)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := strings.TrimSpace(string(body))
		if strings.Contains(strings.ToLower(bodyStr), "not found") {
			return fmt.Errorf("%w: %s", snapshot.ErrPodNotFound, bodyStr)
		}
		return fmt.Errorf("pod remove failed (HTTP %d): %s", resp.StatusCode, bodyStr)
	}
	return nil
}
