package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/version"
)

const actor = "lstk"

type PlatformAPI interface {
	CreateAuthRequest(ctx context.Context) (*AuthRequest, error)
	CheckAuthRequestConfirmed(ctx context.Context, id, exchangeToken string) (bool, error)
	ExchangeAuthRequest(ctx context.Context, id, exchangeToken string) (string, error)
	GetLicenseToken(ctx context.Context, bearerToken string) (string, error)
	GetLicense(ctx context.Context, req *LicenseRequest) (*LicenseResponse, error)
}

type AuthRequest struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	ExchangeToken string `json:"exchange_token"`
}

type authRequestStatus struct {
	Confirmed bool `json:"confirmed"`
}

type authTokenResponse struct {
	ID        string `json:"id"`
	AuthToken string `json:"auth_token"`
}

type licenseTokenResponse struct {
	Token string `json:"token"`
}

type LicenseRequest struct {
	Product     ProductInfo     `json:"product"`
	Credentials CredentialsInfo `json:"credentials"`
	Machine     MachineInfo     `json:"machine"`
}

type ProductInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CredentialsInfo struct {
	Token string `json:"token"`
}

type MachineInfo struct {
	Hostname        string `json:"hostname,omitempty"`
	Platform        string `json:"platform,omitempty"`
	PlatformRelease string `json:"platform_release,omitempty"`
}

type LicenseResponse struct {
	LicenseType string          `json:"license_type"`
	RawBytes    json.RawMessage `json:"-"`
}

var planDisplayNames = map[string]string{
	"hobby":      "Hobby",
	"pro":        "Pro",
	"team":       "Teams",
	"enterprise": "Enterprise",
	"trial":      "Trial",
	"freemium":   "Community",
	"base":       "Starter",
	"ultimate":   "Ultimate",
	"student":    "Student",
}

// PlanDisplayName returns a human-readable plan name for the license type.
// Returns an empty string for a nil receiver or unknown types.
func (r *LicenseResponse) PlanDisplayName() string {
	if r == nil {
		return ""
	}
	if name, ok := planDisplayNames[r.LicenseType]; ok {
		return name
	}
	return r.LicenseType
}

// LicenseError is returned when license validation fails.
// Message is user-friendly; Detail contains the raw server response for debugging.
// IsUnsupportedTag is set when the server rejects the image tag format (a 400 whose
// detail carries the licensing.license.format error code). It means "the server
// cannot judge this tag", not that the license was rejected — the start pre-flight
// skips validation entirely on it and defers to the container's own license check,
// so keep the detection narrow: widening it widens that bypass.
type LicenseError struct {
	Status           int
	Message          string
	Detail           string
	IsUnsupportedTag bool
}

func (e *LicenseError) Error() string {
	return fmt.Sprintf("license validation failed: %s", e.Message)
}

type CloudPod struct {
	Name        string
	Version     int
	LastChanged *time.Time
}

// ErrCloudPodNotFound is returned by GetCloudPod when the platform reports the
// requested pod does not exist (HTTP 404).
var ErrCloudPodNotFound = errors.New("cloud pod not found")

// CloudPodResourceCount is a count of a single resource kind within a service,
// e.g. {Noun: "buckets", Count: 3}.
type CloudPodResourceCount struct {
	Noun  string
	Count int
}

// CloudPodResource groups the resource counts of a single service.
type CloudPodResource struct {
	Service string
	Counts  []CloudPodResourceCount
}

// CloudPodDetails is the metadata for a single cloud snapshot, taken from its
// latest version. Resources is empty when the platform has no resource breakdown
// for the snapshot (e.g. it was saved without resource indexing enabled).
type CloudPodDetails struct {
	Name              string
	Version           int
	Created           *time.Time
	Size              int64
	LocalStackVersion string
	Message           string
	Services          []string
	Resources         []CloudPodResource
}

type PlatformClient struct {
	baseURL    string
	httpClient *http.Client
	logger     log.Logger
}

func NewPlatformClient(apiEndpoint string, logger log.Logger) *PlatformClient {
	return &PlatformClient{
		baseURL: apiEndpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: otelhttp.NewTransport(
				http.DefaultTransport,
				otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
					return "platform " + r.Method + " " + r.URL.Path
				}),
			),
		},
		logger: logger,
	}
}

func (c *PlatformClient) CreateAuthRequest(ctx context.Context) (*AuthRequest, error) {
	payload := map[string]string{
		"actor":   actor,
		"version": version.Version(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/auth/request", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to create auth request: status %d", resp.StatusCode)
	}

	var authReq AuthRequest
	if err := json.NewDecoder(resp.Body).Decode(&authReq); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &authReq, nil
}

func (c *PlatformClient) CheckAuthRequestConfirmed(ctx context.Context, id, exchangeToken string) (bool, error) {
	url := fmt.Sprintf("%s/v1/auth/request/%s?exchange_token=%s", c.baseURL, id, exchangeToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check auth request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to check auth request: status %d", resp.StatusCode)
	}

	var status authRequestStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	return status.Confirmed, nil
}

func (c *PlatformClient) ExchangeAuthRequest(ctx context.Context, id, exchangeToken string) (string, error) {
	url := fmt.Sprintf("%s/v1/auth/request/%s/exchange", c.baseURL, id)
	body, err := json.Marshal(map[string]string{"exchange_token": exchangeToken})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange auth request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to exchange auth request: status %d", resp.StatusCode)
	}

	var tokenResp authTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return tokenResp.AuthToken, nil
}

func (c *PlatformClient) GetLicenseToken(ctx context.Context, bearerToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/license/credentials", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", bearerToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get license token: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("failed to get license token: status %d", resp.StatusCode)
	}

	var tokenResp licenseTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return tokenResp.Token, nil
}

func (c *PlatformClient) GetLicense(ctx context.Context, licReq *LicenseRequest) (*LicenseResponse, error) {
	body, err := json.Marshal(licReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/license/request", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request license: %w", err)
	}

	statusCode := resp.StatusCode

	if statusCode == http.StatusOK {
		rawBytes, err := io.ReadAll(resp.Body)
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read license response: %w", err)
		}
		var licResp LicenseResponse
		if err := json.Unmarshal(rawBytes, &licResp); err != nil {
			return nil, fmt.Errorf("failed to decode license response: %w", err)
		}
		licResp.RawBytes = rawBytes
		return &licResp, nil
	}

	var detail string
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		detail = fmt.Sprintf("failed to read response body: %v", err)
	} else {
		detail = string(bytes.TrimSpace(respBody))
	}
	if err := resp.Body.Close(); err != nil {
		c.logger.Error("failed to close response body: %v", err)
	}

	switch statusCode {
	case http.StatusBadRequest:
		if strings.Contains(detail, "licensing.license.format") {
			return nil, &LicenseError{
				Status:           statusCode,
				Message:          "image tag not accepted by the license server",
				Detail:           detail,
				IsUnsupportedTag: true,
			}
		}
		return nil, &LicenseError{
			Status:  statusCode,
			Message: "invalid token format, missing license assignment, or missing subscription",
			Detail:  detail,
		}
	case http.StatusForbidden:
		return nil, &LicenseError{
			Status:  statusCode,
			Message: "invalid, inactive, or expired authentication token or subscription",
			Detail:  detail,
		}
	default:
		return nil, &LicenseError{
			Status:  statusCode,
			Message: fmt.Sprintf("unexpected status %d", statusCode),
			Detail:  detail,
		}
	}
}

func (c *PlatformClient) ListCloudPods(ctx context.Context, authToken, creator string) ([]CloudPod, error) {
	u := c.baseURL + "/v1/cloudpods"
	if creator != "" {
		// "" lists all org pods; "me" filters server-side to the current user.
		u += "?" + url.Values{"creator": {creator}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list cloud pods: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to list cloud pods: status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	// The platform returns a bare top-level array, not an object wrapping the entries.
	var raw []struct {
		PodName    string `json:"pod_name"`
		MaxVersion int    `json:"max_version"`
		LastChange *int64 `json:"last_change"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode cloud pods response: %w", err)
	}

	pods := make([]CloudPod, len(raw))
	for i, p := range raw {
		pods[i] = CloudPod{Name: p.PodName, Version: p.MaxVersion}
		if p.LastChange != nil {
			t := time.Unix(*p.LastChange, 0)
			pods[i].LastChanged = &t
		}
	}
	return pods, nil
}

// rawCloudPodVersion mirrors a single entry in the platform's "versions" array.
// The platform reports the byte size as "storage_size"; "size" is accepted as a
// fallback for forward/backward compatibility. The created timestamp is captured
// raw and parsed leniently since its key and encoding vary.
type rawCloudPodVersion struct {
	Version               int             `json:"version"`
	LocalStackVersion     string          `json:"localstack_version"`
	Services              []string        `json:"services"`
	StorageSize           int64           `json:"storage_size"`
	Size                  int64           `json:"size"`
	Description           string          `json:"description"`
	CreatedAt             json.RawMessage `json:"created_at"`
	LastChange            json.RawMessage `json:"last_change"`
	CloudControlResources string          `json:"cloud_control_resources"`
}

// size returns the version's byte size, preferring storage_size.
func (v rawCloudPodVersion) size() int64 {
	if v.StorageSize > 0 {
		return v.StorageSize
	}
	return v.Size
}

type rawCloudPod struct {
	PodName     string               `json:"pod_name"`
	MaxVersion  int                  `json:"max_version"`
	StorageSize int64                `json:"storage_size"`
	Versions    []rawCloudPodVersion `json:"versions"`
}

// GetCloudPod fetches metadata for a single cloud snapshot from the platform.
// It returns ErrCloudPodNotFound when the pod does not exist.
func (c *PlatformClient) GetCloudPod(ctx context.Context, authToken, podName string) (*CloudPodDetails, error) {
	u := c.baseURL + "/v1/cloudpods/" + url.PathEscape(podName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+authToken)))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud pod: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrCloudPodNotFound
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to get cloud pod: status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var raw rawCloudPod
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode cloud pod response: %w", err)
	}
	return raw.toDetails(podName), nil
}

// toDetails projects the latest version's metadata into CloudPodDetails.
func (r rawCloudPod) toDetails(fallbackName string) *CloudPodDetails {
	name := r.PodName
	if name == "" {
		name = fallbackName
	}
	details := &CloudPodDetails{Name: name, Version: r.MaxVersion}

	v := r.latestVersion()
	if v == nil {
		return details
	}
	if v.Version != 0 {
		details.Version = v.Version
	}
	details.Size = v.size()
	if details.Size == 0 {
		details.Size = r.StorageSize
	}
	details.LocalStackVersion = v.LocalStackVersion
	details.Message = v.Description
	details.Services = v.Services
	if t := parseFlexibleTime(v.CreatedAt); t != nil {
		details.Created = t
	} else if t := parseFlexibleTime(v.LastChange); t != nil {
		details.Created = t
	}
	details.Resources = resourceCountsFromCloudControl(v.CloudControlResources)
	return details
}

// latestVersion returns the version matching MaxVersion, falling back to the last
// entry in the array.
func (r rawCloudPod) latestVersion() *rawCloudPodVersion {
	if len(r.Versions) == 0 {
		return nil
	}
	for i := range r.Versions {
		if r.Versions[i].Version == r.MaxVersion {
			return &r.Versions[i]
		}
	}
	return &r.Versions[len(r.Versions)-1]
}

// parseFlexibleTime parses a timestamp encoded either as a Unix epoch number or
// an RFC3339 string. Returns nil when the value is absent or unrecognized.
func parseFlexibleTime(raw json.RawMessage) *time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var epoch int64
	if err := json.Unmarshal(raw, &epoch); err == nil {
		t := time.Unix(epoch, 0).UTC()
		return &t
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

// resourceCountsFromCloudControl decodes the cloud_control_resources JSON string
// (a map of CloudFormation type → resource entries) into per-service counts.
// Any decoding problem yields an empty result so callers never fail on it.
func resourceCountsFromCloudControl(raw string) []CloudPodResource {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var byType map[string][]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &byType); err != nil {
		return nil
	}

	// service → singular noun → count. The map keys stay singular; each noun is
	// pluralized below based on its final count (e.g. "1 topic", "5 queues").
	counts := map[string]map[string]int{}
	for cfnType, entries := range byType {
		parts := strings.Split(cfnType, "::")
		if len(parts) < 3 {
			continue
		}
		service := strings.ToLower(parts[1])
		noun := strings.ToLower(parts[len(parts)-1])
		if counts[service] == nil {
			counts[service] = map[string]int{}
		}
		counts[service][noun] += len(entries)
	}

	services := make([]string, 0, len(counts))
	for s := range counts {
		services = append(services, s)
	}
	sort.Strings(services)

	resources := make([]CloudPodResource, 0, len(services))
	for _, s := range services {
		nouns := make([]string, 0, len(counts[s]))
		for n := range counts[s] {
			nouns = append(nouns, n)
		}
		sort.Strings(nouns)
		nc := make([]CloudPodResourceCount, 0, len(nouns))
		for _, n := range nouns {
			nc = append(nc, CloudPodResourceCount{Noun: pluralizeFor(n, counts[s][n]), Count: counts[s][n]})
		}
		resources = append(resources, CloudPodResource{Service: s, Counts: nc})
	}
	return resources
}

// pluralizeFor returns the singular noun for a count of one and the plural form
// otherwise (1 topic, 2 topics).
func pluralizeFor(noun string, count int) string {
	if count == 1 {
		return noun
	}
	return pluralize(noun)
}

// pluralize applies simple English pluralization sufficient for AWS resource
// nouns (bucket→buckets, policy→policies, queue→queues).
func pluralize(noun string) string {
	if len(noun) < 2 {
		return noun
	}
	switch {
	case strings.HasSuffix(noun, "s"), strings.HasSuffix(noun, "x"), strings.HasSuffix(noun, "z"),
		strings.HasSuffix(noun, "ch"), strings.HasSuffix(noun, "sh"):
		return noun + "es"
	case strings.HasSuffix(noun, "y") && !isVowel(noun[len(noun)-2]):
		return noun[:len(noun)-1] + "ies"
	default:
		return noun + "s"
	}
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}
