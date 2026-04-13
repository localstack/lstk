package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	GetLatestCatalogVersion(ctx context.Context, emulatorType string) (string, error)
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
	LicenseType string `json:"license_type"`
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
type LicenseError struct {
	Status  int
	Message string
	Detail  string
}

func (e *LicenseError) Error() string {
	return fmt.Sprintf("license validation failed: %s", e.Message)
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
			Timeout:   30 * time.Second,
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
		var licResp LicenseResponse
		decErr := json.NewDecoder(resp.Body).Decode(&licResp)
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
		if decErr != nil {
			return nil, fmt.Errorf("failed to decode license response: %w", decErr)
		}
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

type catalogVersionResponse struct {
	EmulatorType string `json:"emulator_type"`
	Version      string `json:"version"`
}

func (c *PlatformClient) GetLatestCatalogVersion(ctx context.Context, emulatorType string) (string, error) {
	reqURL := fmt.Sprintf("%s/v1/license/catalog/%s/version", c.baseURL, url.PathEscape(emulatorType))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get catalog version: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get catalog version: status %d", resp.StatusCode)
	}

	var versionResp catalogVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if versionResp.EmulatorType == "" || versionResp.Version == "" {
		return "", fmt.Errorf("incomplete catalog response: emulator_type=%q version=%q", versionResp.EmulatorType, versionResp.Version)
	}

	if versionResp.EmulatorType != emulatorType {
		return "", fmt.Errorf("unexpected emulator_type: got=%q want=%q", versionResp.EmulatorType, emulatorType)
	}

	return versionResp.Version, nil
}
