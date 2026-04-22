package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PlatformClient talks to the LocalStack platform API for remote snapshot management.
// Auth header: ls-api-key: <token>
type PlatformClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewPlatformClient(apiEndpoint, token string) *PlatformClient {
	return &PlatformClient{
		baseURL: apiEndpoint,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// podResponse is the raw platform API shape for a remote snapshot.
type podResponse struct {
	PodName     string            `json:"pod_name"`
	MaxVersion  int               `json:"max_version"`
	LastChange  int64             `json:"last_change"`
	IsPublic    bool              `json:"is_public"`
	StorageSize int64             `json:"storage_size"`
	Versions    []versionResponse `json:"versions"`
}

type versionResponse struct {
	Version           int      `json:"version"`
	CreatedAt         int64    `json:"created_at"`
	LocalStackVersion *string  `json:"localstack_version"`
	Services          []string `json:"services"`
	Description       *string  `json:"description"`
	StorageSize       int64    `json:"storage_size"`
}

func (p *podResponse) toPodInfo() PodInfo {
	pi := PodInfo{
		Name:        p.PodName,
		MaxVersion:  p.MaxVersion,
		LastChange:  p.LastChange,
		IsPublic:    p.IsPublic,
		StorageSize: p.StorageSize,
	}
	for _, v := range p.Versions {
		pi.Versions = append(pi.Versions, v.toVersionInfo())
	}
	return pi
}

func (v *versionResponse) toVersionInfo() VersionInfo {
	vi := VersionInfo{
		Version:     v.Version,
		CreatedAt:   v.CreatedAt,
		StorageSize: v.StorageSize,
	}
	if v.LocalStackVersion != nil {
		vi.LocalStackVersion = *v.LocalStackVersion
	}
	if v.Description != nil {
		vi.Description = *v.Description
	}
	// Filter out empty service strings that can appear in older pods.
	for _, s := range v.Services {
		if s != "" {
			vi.Services = append(vi.Services, s)
		}
	}
	return vi
}

// ListPods returns all remote snapshots for the authenticated user.
func (c *PlatformClient) ListPods(ctx context.Context) ([]PodInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/cloudpods", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list request: %w", err)
	}
	req.Header.Set("ls-api-key", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed: verify your token is valid")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw []podResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode list response: %w", err)
	}

	pods := make([]PodInfo, len(raw))
	for i, p := range raw {
		pods[i] = p.toPodInfo()
	}
	return pods, nil
}

// GetPod returns a single remote snapshot with its versions.
func (c *PlatformClient) GetPod(ctx context.Context, name string) (*PodInfo, error) {
	reqURL := c.baseURL + "/v1/cloudpods/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get request: %w", err)
	}
	req.Header.Set("ls-api-key", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed: verify your token is valid")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("snapshot %q not found", name)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("get failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw podResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode get response: %w", err)
	}
	info := raw.toPodInfo()
	return &info, nil
}

// DeletePod deletes a remote snapshot and all its versions.
func (c *PlatformClient) DeletePod(ctx context.Context, name string) error {
	reqURL := c.baseURL + "/v1/cloudpods/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}
	req.Header.Set("ls-api-key", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed: verify your token is valid")
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("snapshot %q not found", name)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("delete failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
