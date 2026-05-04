package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
)

const (
	instancesPath     = "/v1/compute/instances"
	instancePath      = "/v1/compute/instances/%s"
	instanceLogsPath  = "/v1/compute/instances/%s/logs"
	stateResetAPIPath = "/_localstack/state/reset"
)

var ErrNotFound = errors.New("sandbox instance not found")

type CreateOptions struct {
	Name            string
	LifetimeMinutes int
	EnvVars         map[string]string
}

type Instance struct {
	Name     string `json:"instance_name"`
	Status   string `json:"status"`
	Endpoint string `json:"endpoint_url"`
	Expires  string `json:"expiry_time"`
}

type Client struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
	logger     log.Logger
}

func NewClient(baseURL, authToken string, logger log.Logger) *Client {
	if logger == nil {
		logger = log.Nop()
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authToken:  authToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

func (c *Client) Create(ctx context.Context, opts CreateOptions) (json.RawMessage, error) {
	payload := struct {
		InstanceName string            `json:"instance_name"`
		Lifetime     int               `json:"lifetime"`
		EnvVars      map[string]string `json:"env_vars"`
	}{
		InstanceName: opts.Name,
		Lifetime:     opts.LifetimeMinutes,
		EnvVars:      opts.EnvVars,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode create request: %w", err)
	}
	respBody, err := c.do(ctx, http.MethodPost, c.baseURL+instancesPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create sandbox instance: %w", err)
	}
	return respBody, nil
}

func (c *Client) List(ctx context.Context) ([]Instance, error) {
	body, err := c.do(ctx, http.MethodGet, c.baseURL+instancesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("list sandbox instances: %w", err)
	}
	return parseInstances(body)
}

// Describe fetches instance state. Returns ErrNotFound on 404.
func (c *Client) Describe(ctx context.Context, name string) (Instance, error) {
	reqURL := fmt.Sprintf(c.baseURL+instancePath, url.PathEscape(name))
	body, err := c.do(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Instance{}, ErrNotFound
		}
		return Instance{}, fmt.Errorf("describe sandbox instance: %w", err)
	}
	return parseInstance(body)
}

func (c *Client) Delete(ctx context.Context, name string) error {
	reqURL := fmt.Sprintf(c.baseURL+instancePath, url.PathEscape(name))
	if _, err := c.do(ctx, http.MethodDelete, reqURL, nil); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("delete sandbox instance: %w", err)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, name string) ([]string, error) {
	reqURL := fmt.Sprintf(c.baseURL+instanceLogsPath, url.PathEscape(name))
	body, err := c.do(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("fetch sandbox logs: %w", err)
	}
	return parseLogLines(body)
}

func (c *Client) ResetState(ctx context.Context, endpointURL string) error {
	endpointURL = strings.TrimRight(endpointURL, "/")
	if _, err := c.doNoAuth(ctx, http.MethodPost, endpointURL+stateResetAPIPath, nil); err != nil {
		return fmt.Errorf("reset sandbox state: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, url string, body io.Reader) (json.RawMessage, error) {
	return c.doRequest(ctx, method, url, body, true)
}

func (c *Client) doNoAuth(ctx context.Context, method, url string, body io.Reader) (json.RawMessage, error) {
	return c.doRequest(ctx, method, url, body, false)
}

func (c *Client) doRequest(ctx context.Context, method, url string, body io.Reader, platformAuth bool) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if platformAuth {
		c.setAuth(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		detail, _ := io.ReadAll(resp.Body)
		if len(detail) > 0 {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return respBody, nil
}

// WaitForDeletion polls Describe until the instance returns 404 or timeout elapses.
// Transient request errors are swallowed and retried.
func (c *Client) WaitForDeletion(ctx context.Context, sink output.Sink, name string, timeout time.Duration) error {
	sink.Emit(output.SpinnerStart(fmt.Sprintf("Waiting for instance %q to be deleted", name)))
	defer sink.Emit(output.SpinnerStop())

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for instance %q to be deleted", timeout, name)
		}

		_, err := c.Describe(ctx, name)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			c.logger.Info("sandbox poll error (will retry): %v", err)
		}

		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) setAuth(req *http.Request) {
	if c.authToken == "" {
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(":" + c.authToken))
	req.Header.Set("Authorization", "Basic "+encoded)
}

func (c *Client) closeBody(body io.ReadCloser) {
	if err := body.Close(); err != nil {
		c.logger.Error("failed to close response body: %v", err)
	}
}

func parseInstances(body []byte) ([]Instance, error) {
	var items []any
	if err := json.Unmarshal(body, &items); err == nil {
		instances := make([]Instance, 0, len(items))
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			instances = append(instances, instanceFromMap(m))
		}
		return instances, nil
	}
	var wrapped map[string][]map[string]any
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	for _, key := range []string{"instances", "items", "data"} {
		if v, ok := wrapped[key]; ok {
			return instancesFromMaps(v), nil
		}
	}
	return nil, fmt.Errorf("sandbox list response did not contain instances")
}

func parseInstance(body []byte) (Instance, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return Instance{}, err
	}
	return instanceFromMap(m), nil
}

func instancesFromMaps(maps []map[string]any) []Instance {
	instances := make([]Instance, 0, len(maps))
	for _, m := range maps {
		instances = append(instances, instanceFromMap(m))
	}
	return instances
}

func instanceFromMap(m map[string]any) Instance {
	return Instance{
		Name:     fieldString(m, "instance_name", "instanceName", "name", "id"),
		Status:   fieldString(m, "status"),
		Endpoint: fieldString(m, "endpoint_url", "endpointUrl", "endpoint"),
		Expires:  fieldString(m, "expiry_time", "expiryTime", "expires_at", "expiresAt", "expires"),
	}
}

func parseLogLines(body []byte) ([]string, error) {
	var records []map[string]any
	if err := json.Unmarshal(body, &records); err == nil {
		lines := make([]string, 0, len(records))
		for _, record := range records {
			line := fieldString(record, "content", "message", "line")
			if line != "" {
				lines = append(lines, line)
			}
		}
		return lines, nil
	}
	var lines []string
	if err := json.Unmarshal(body, &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

func fieldString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return stringify(value)
		}
	}
	return ""
}

func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case map[string]any:
		return compactJSON(v)
	case []any:
		return compactJSON(v)
	default:
		return fmt.Sprint(v)
	}
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
