package feedback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/update"
	"github.com/localstack/lstk/internal/version"
)

const DefaultAPIEndpoint = "https://api.localstack.cloud"

type Client struct {
	httpClient *http.Client
	endpoint   string
}

type SubmitInput struct {
	Message   string
	AuthToken string
	Context   Context
}

type Context struct {
	AuthConfigured   bool
	InstallMethod    string
	Shell            string
	ContainerRuntime string
	ConfigPath       string
}

type submitRequest struct {
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata"`
}

func NewClient(endpoint string) *Client {
	if endpoint == "" {
		endpoint = DefaultAPIEndpoint
	}
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		endpoint:   strings.TrimRight(endpoint, "/"),
	}
}

func (c *Client) Submit(ctx context.Context, input SubmitInput) error {
	body := submitRequest{
		Message:  strings.TrimSpace(input.Message),
		Metadata: buildMetadata(input.Context),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/feedback", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", basicAuthToken(input.AuthToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		detail := strings.TrimSpace(string(respBody))
		if detail == "" {
			return fmt.Errorf("feedback API returned %s", resp.Status)
		}
		return fmt.Errorf("feedback API returned %s: %s", resp.Status, detail)
	}
	return nil
}

func buildMetadata(ctx Context) map[string]any {
	return map[string]any{
		"version (lstk)":    version.Version(),
		"os (arch)":         runtime.GOOS + " (" + runtime.GOARCH + ")",
		"installation":      orUnknown(ctx.InstallMethod),
		"shell":             orUnknown(ctx.Shell),
		"container runtime": orUnknown(ctx.ContainerRuntime),
		"auth":              authLabel(ctx.AuthConfigured),
		"config":            orUnknown(ctx.ConfigPath),
	}
}

func authLabel(v bool) string {
	if v {
		return "Configured"
	}
	return "Not Configured"
}

func orUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}

func DetectInstallMethod() string {
	return update.DetectInstallMethod().Method.String()
}

func basicAuthToken(token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+token))
}
