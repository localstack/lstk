package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type PlatformAPI interface {
	CreateAuthRequest(ctx context.Context) (*AuthRequest, error)
	CheckAuthRequestConfirmed(ctx context.Context, id, exchangeToken string) (bool, error)
	ExchangeAuthRequest(ctx context.Context, id, exchangeToken string) (string, error)
	GetLicenseToken(ctx context.Context, bearerToken string) (string, error)
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

type PlatformClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPlatformClient() *PlatformClient {
	return &PlatformClient{
		baseURL:    "https://api.localstack.cloud",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *PlatformClient) CreateAuthRequest(ctx context.Context) (*AuthRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/auth/request", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close response body: %v", err)
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
			log.Printf("failed to close response body: %v", err)
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
			log.Printf("failed to close response body: %v", err)
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
			log.Printf("failed to close response body: %v", err)
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
