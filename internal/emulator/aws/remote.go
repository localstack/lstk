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
	"strings"

	"github.com/localstack/lstk/internal/snapshot"
)

// remotePayload is the "remote" object sent in pod save/load/list request bodies
// to target a named remote with ephemeral credential params.
type remotePayload struct {
	RemoteName   string            `json:"remote_name"`
	RemoteParams map[string]string `json:"remote_params,omitempty"`
}

// podRequestBody is the JSON body for pod save/load/list. Remote is omitted for
// platform (pod:) operations, in which case the body is "{}".
type podRequestBody struct {
	Remote *remotePayload `json:"remote,omitempty"`
}

// marshalPodBody builds the request body for a pod operation. When remoteName is
// empty it returns "{}" (the platform default remote).
func marshalPodBody(remoteName string, params map[string]string) ([]byte, error) {
	body := podRequestBody{}
	if remoteName != "" {
		body.Remote = &remotePayload{RemoteName: remoteName, RemoteParams: params}
	}
	return json.Marshal(body)
}

// setBasicAuth sets the LocalStack Basic auth header when a token is present.
// S3 remotes do not require a platform token, so it is optional.
func setBasicAuth(req *http.Request, authToken string) {
	if authToken == "" {
		return
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))
}

// S3BucketExists reports whether an S3 bucket exists, via an unsigned HEAD to the
// S3 endpoint: a 404 means the bucket does not exist; any other status (200, 403,
// or a redirect for a bucket in another region) means it does. This lets lstk
// reject a missing bucket up front instead of letting the emulator auto-create it.
func (c *Client) S3BucketExists(ctx context.Context, bucket string) (bool, error) {
	url := fmt.Sprintf(c.s3BucketURLTemplate, bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("connect to S3: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return true, nil
}

// RegisterRemote upserts a named remote on the running emulator. The emulator
// persists it (idempotently replacing any same-named entry) so subsequent
// save/load/list calls can reference it by name.
func (c *Client) RegisterRemote(ctx context.Context, host, name, remoteURL string) error {
	url := fmt.Sprintf("http://%s/_localstack/pods/remotes/%s", host, name)
	payload, err := json.Marshal(map[string]any{
		"name":       name,
		"protocols":  []string{"s3"},
		"remote_url": remoteURL,
	})
	if err != nil {
		return fmt.Errorf("marshal remote request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register remote failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// SavePodRemote saves the running state to podName on the named remote.
func (c *Client) SavePodRemote(ctx context.Context, host, podName, remoteName string, params map[string]string, authToken string) (snapshot.PodSaveResult, error) {
	body, err := marshalPodBody(remoteName, params)
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("marshal request: %w", err)
	}
	return c.doPodSave(ctx, host, podName, authToken, body)
}

// LoadPodRemote loads podName from the named remote with the given merge strategy.
func (c *Client) LoadPodRemote(ctx context.Context, host, podName, remoteName string, params map[string]string, authToken, strategy string) ([]string, error) {
	body, err := marshalPodBody(remoteName, params)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return c.doPodLoad(ctx, host, podName, authToken, strategy, body)
}

// ListPodsRemote lists the snapshots stored on the named remote via
// GET /_localstack/pods (with the remote passed in the request body).
func (c *Client) ListPodsRemote(ctx context.Context, host, remoteName string, params map[string]string, authToken, creator string) ([]snapshot.RemotePod, error) {
	url := fmt.Sprintf("http://%s/_localstack/pods", host)
	if creator != "" {
		url += "?creator=" + creator
	}
	body, err := marshalPodBody(remoteName, params)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBasicAuth(req, authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list pods failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		CloudPods []struct {
			PodName    string `json:"pod_name"`
			MaxVersion int    `json:"max_version"`
		} `json:"cloudpods"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	pods := make([]snapshot.RemotePod, len(parsed.CloudPods))
	for i, p := range parsed.CloudPods {
		pods[i] = snapshot.RemotePod{Name: p.PodName, MaxVersion: p.MaxVersion}
	}
	return pods, nil
}

// doPodSave issues POST /_localstack/pods/{name} with the given JSON body and
// parses the NDJSON response stream into a PodSaveResult.
func (c *Client) doPodSave(ctx context.Context, host, podName, authToken string, body []byte) (snapshot.PodSaveResult, error) {
	url := fmt.Sprintf("http://%s/_localstack/pods/%s", host, podName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBasicAuth(req, authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return snapshot.PodSaveResult{}, fmt.Errorf("pod save failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
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

// doPodLoad issues PUT /_localstack/pods/{name}[?merge=strategy] with the given
// JSON body and parses the NDJSON response stream into the list of services.
func (c *Client) doPodLoad(ctx context.Context, host, podName, authToken, strategy string, body []byte) ([]string, error) {
	url := fmt.Sprintf("http://%s/_localstack/pods/%s", host, podName)
	if strategy != "" {
		url += "?merge=" + strategy
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBasicAuth(req, authToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: %s", snapshot.ErrIncompatibleSnapshot, strings.TrimSpace(string(respBody)))
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pod load failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
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
