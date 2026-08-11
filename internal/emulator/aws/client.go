package aws

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/localstack/lstk/internal/snapshot"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/localstack/lstk/internal/emulator"
)

type Client struct {
	http *http.Client
	// s3BucketURLTemplate builds the URL for an S3 bucket existence check; it
	// contains a single %s for the bucket name. Overridable in tests.
	s3BucketURLTemplate string
}

// isFeatureUnavailableResponse reports whether a non-2xx response from a
// /_localstack/pods* endpoint means the snapshot feature itself isn't available
// (the license doesn't cover it), rather than the operation having failed. When
// unentitled, the emulator never registers these routes at all, so every method
// falls through to the generic unmatched-path handler, which replies with a bare
// 404 and no body — a shape every real pods error can be told apart from, since
// those always carry a message (or arrive as an NDJSON error event).
//
// The paths these calls target are hardcoded, never user-supplied, so a URL typo
// cannot reach this and be misreported as an entitlement problem.
//
// Emulator-side the entitlement is the licensed plux plugin
// localstack.platform.plugin/pods ("Cloud Pods" on the license), which raises
// PluginDisabled at init. That is why there is no 402/403 to key off: nothing
// per-request ever checks the license. --merge=overwrite trips a separate plugin,
// localstack.platform.plugin/state-reset.
//
// Keep the discriminator narrow, and keep the detection reactive:
//   - The sibling GET /_localstack/pods/{name}/versions (unused by lstk today) answers
//     a genuinely missing pod with a bare message-less 404, so widening to it would
//     report a mistyped pod name as a billing problem.
//   - Don't pre-check the cached license instead: its products[] list is coarse and the
//     pods product string can't be verified from either repo, so a local check risks
//     blocking paying customers.
func isFeatureUnavailableResponse(statusCode int, body []byte) bool {
	return statusCode == http.StatusNotFound && len(bytes.TrimSpace(body)) == 0
}

// emulatorStatusError formats a non-2xx emulator response. header is the whole
// message up to (but excluding) the body, e.g. "LocalStack returned status 404".
// The body is appended only when non-empty, so a response with no body never
// renders a dangling ": ".
func emulatorStatusError(header string, body []byte) error {
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		return fmt.Errorf("%s: %s", header, trimmed)
	}
	return errors.New(header)
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
		s3BucketURLTemplate: "https://%s.s3.amazonaws.com/",
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

func (c *Client) FetchVersion(ctx context.Context, baseURL string) (string, error) {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/health"
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

func (c *Client) FetchResources(ctx context.Context, baseURL string) ([]emulator.Resource, error) {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/resources"
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
	nd := newNDJSONReader(resp.Body)
	for {
		line, ok, err := nd.next()
		if err != nil {
			return nil, fmt.Errorf("failed to read resources stream: %w", err)
		}
		if !ok {
			break
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

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Service != rows[j].Service {
			return rows[i].Service < rows[j].Service
		}
		return rows[i].Name < rows[j].Name
	})

	return rows, nil
}

func (c *Client) ResetState(ctx context.Context, baseURL string) error {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/state/reset"
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
		body, _ := io.ReadAll(resp.Body)
		if isFeatureUnavailableResponse(resp.StatusCode, body) {
			return snapshot.ErrSnapshotFeatureUnavailable
		}
		return emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), body)
	}
	return nil
}

// ExportState streams the running instance's state into dst as a zip. services,
// when non-empty, limits the export to that subset of services. It returns the
// services actually captured, reported by LocalStack via a response header.
//
// The services query param is the same per-service filter the pod endpoints take as
// attributes.services — a different endpoint, not different emulator behaviour.
func (c *Client) ExportState(ctx context.Context, baseURL string, services []string, dst io.Writer) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/pods/state"
	if len(services) > 0 {
		// Safe to concatenate unescaped: validate.ServiceList restricts each
		// item to [\w-]+, which contains no query-string metacharacters.
		url += "?services=" + strings.Join(services, ",")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if isFeatureUnavailableResponse(resp.StatusCode, body) {
			return nil, snapshot.ErrSnapshotFeatureUnavailable
		}
		return nil, emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), body)
	}

	if _, err := io.Copy(dst, resp.Body); err != nil {
		return nil, fmt.Errorf("stream state: %w", err)
	}

	var extracted []string
	if v := resp.Header.Get("x-localstack-pod-services"); v != "" {
		extracted = strings.Split(v, ",")
	}
	return extracted, nil
}

func (c *Client) ImportState(ctx context.Context, baseURL string, src io.Reader, strategy string) error {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/pods"
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
		if isFeatureUnavailableResponse(resp.StatusCode, body) {
			return snapshot.ErrSnapshotFeatureUnavailable
		}
		return emulatorStatusError(fmt.Sprintf("LocalStack returned status %d", resp.StatusCode), body)
	}

	nd := newNDJSONReader(resp.Body)
	for {
		line, ok, err := nd.next()
		if err != nil {
			return err
		}
		if !ok {
			break
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
	return nil
}

// ndjsonReader reads newline-delimited JSON streams without the fixed
// token-size limit of bufio.Scanner (which errors with "token too long" on any
// single line larger than its buffer). Lines grow to whatever size the stream
// produces, so a large JSON object on one line is read whole and can be parsed.
type ndjsonReader struct {
	r *bufio.Reader
}

func newNDJSONReader(r io.Reader) *ndjsonReader {
	return &ndjsonReader{r: bufio.NewReader(r)}
}

// next returns the next non-empty, trimmed line. ok is false at end of stream.
// A read error other than io.EOF is returned in err.
func (n *ndjsonReader) next() (line string, ok bool, err error) {
	for {
		s, readErr := n.r.ReadString('\n')
		s = strings.TrimSpace(s)
		if s != "" {
			return s, true, nil
		}
		if readErr != nil {
			if readErr == io.EOF {
				return "", false, nil
			}
			return "", false, readErr
		}
	}
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

// isPodVersionNotFoundMsg reports whether an emulator error message indicates the
// requested version of an existing pod does not exist. The emulator answers with
// "Unable to load pod X with version N. The maximum version available in the
// remote storage is M" — we match on the distinctive tail and pass the whole
// message through, since it already names the highest available version.
func isPodVersionNotFoundMsg(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "maximum version available")
}

func (c *Client) LoadPodSnapshot(ctx context.Context, baseURL, podName string, version int, authToken, strategy string) ([]string, error) {
	return c.doPodLoad(ctx, baseURL, podName, version, authToken, strategy, []byte("{}"))
}

func (c *Client) DiffPodSnapshot(ctx context.Context, baseURL, podName string, version int, authToken string) (snapshot.DiffResult, error) {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/pods/" + podName + "/diff"
	if version > 0 {
		url += "?version=" + strconv.Itoa(version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to LocalStack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := strings.TrimSpace(string(body))
		if isPodVersionNotFoundMsg(bodyStr) {
			return nil, fmt.Errorf("%w: %s", snapshot.ErrPodVersionNotFound, bodyStr)
		}
		if isPodNotFoundMsg(bodyStr) {
			return nil, fmt.Errorf("%w: %s", snapshot.ErrPodNotFound, bodyStr)
		}
		if isFeatureUnavailableResponse(resp.StatusCode, body) {
			return nil, snapshot.ErrSnapshotFeatureUnavailable
		}
		return nil, emulatorStatusError(fmt.Sprintf("diff failed (HTTP %d)", resp.StatusCode), body)
	}

	var raw map[string][]struct {
		OperationType string `json:"operation_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse diff response: %w", err)
	}

	result := make(snapshot.DiffResult, len(raw))
	for svc, ops := range raw {
		var counts snapshot.ServiceDiffCounts
		for _, op := range ops {
			switch op.OperationType {
			case "ADDITION":
				counts.Additions++
			case "MODIFICATION":
				counts.Modifications++
			// DELETION is intentionally omitted: the diff endpoint does not currently return deletions.
			}
		}
		result[svc] = counts
	}
	return result, nil
}

// SavePodSnapshot saves the running state to a platform-hosted pod. services,
// when non-empty, limits the save to that subset of services.
func (c *Client) SavePodSnapshot(ctx context.Context, baseURL, podName, authToken string, services []string) (snapshot.PodSaveResult, error) {
	body, err := marshalPodBody("", nil, services)
	if err != nil {
		return snapshot.PodSaveResult{}, fmt.Errorf("marshal request: %w", err)
	}
	return c.doPodSave(ctx, baseURL, podName, authToken, body)
}

func (c *Client) RemovePodSnapshot(ctx context.Context, baseURL, podName, authToken string) error {
	url := strings.TrimRight(baseURL, "/") + "/_localstack/pods/" + podName
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
		if isFeatureUnavailableResponse(resp.StatusCode, body) {
			return snapshot.ErrSnapshotFeatureUnavailable
		}
		return emulatorStatusError(fmt.Sprintf("pod remove failed (HTTP %d)", resp.StatusCode), body)
	}
	return nil
}
