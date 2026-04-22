package snapshot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EmulatorClient talks to the running LocalStack emulator's /_localstack/pods/* endpoints.
type EmulatorClient struct {
	host string // e.g. "localhost.localstack.cloud:4566"
	http *http.Client
}

func NewEmulatorClient(host string) *EmulatorClient {
	return &EmulatorClient{
		host: host,
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *EmulatorClient) baseURL() string {
	return "http://" + c.host + "/_localstack/pods"
}

// Save sends a remote snapshot save request and calls onEvent for each streamed event.
// The token is passed as ls-api-key so the emulator can authenticate with the platform.
func (c *EmulatorClient) Save(ctx context.Context, opts SaveOptions, onEvent func(streamEvent)) error {
	type attributes struct {
		IsPublic    bool     `json:"is_public,omitempty"`
		Description string   `json:"description,omitempty"`
		Services    []string `json:"services,omitempty"`
	}
	type payload struct {
		Attributes attributes `json:"attributes"`
	}

	p := payload{
		Attributes: attributes{
			Description: opts.Message,
			Services:    opts.Services,
		},
	}
	if opts.Visibility == "public" {
		p.Attributes.IsPublic = true
	}

	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal save request: %w", err)
	}

	reqURL := c.baseURL() + "/" + url.PathEscape(opts.PodName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create save request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ls-api-key", opts.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("save failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return readStreamEvents(resp.Body, onEvent)
}

// Load sends a remote snapshot load request and calls onEvent for each streamed event.
func (c *EmulatorClient) Load(ctx context.Context, opts LoadOptions, onEvent func(streamEvent)) error {
	type payload struct{}

	body, err := json.Marshal(payload{})
	if err != nil {
		return fmt.Errorf("failed to marshal load request: %w", err)
	}

	strategy := opts.Strategy
	if strategy == "" {
		strategy = "account-region-merge"
	}

	reqURL := c.baseURL() + "/" + url.PathEscape(opts.PodName)
	q := url.Values{}
	if opts.Version > 0 {
		q.Set("version", fmt.Sprintf("%d", opts.Version))
	}
	q.Set("merge", strategy)
	if opts.DryRun {
		q.Set("dry_run", "true")
	}
	if len(q) > 0 {
		reqURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create load request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ls-api-key", opts.Token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("load failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return readStreamEvents(resp.Body, onEvent)
}

// Export downloads current emulator state as a ZIP and writes it to w.
// onProgress is called periodically with (bytesWritten, totalBytes). totalBytes may be 0 if unknown.
func (c *EmulatorClient) Export(ctx context.Context, services []string, w io.Writer, onProgress func(done, total int64)) error {
	reqURL := c.baseURL() + "/state"
	if len(services) > 0 {
		reqURL += "?services=" + url.QueryEscape(strings.Join(services, ","))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create export request: %w", err)
	}
	req.Header.Set("x-localstack-data", "{}")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to export snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("export failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	total := resp.ContentLength // -1 if unknown

	pr := &progressReader{
		r:          resp.Body,
		onProgress: onProgress,
		total:      total,
	}
	if _, err := io.Copy(w, pr); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}
	return nil
}

// Import uploads a ZIP file to the emulator and calls onEvent for each streamed event.
// size is the total byte count of r (used for progress reporting); pass 0 if unknown.
func (c *EmulatorClient) Import(ctx context.Context, r io.Reader, size int64, onProgress func(done, total int64), onEvent func(streamEvent)) error {
	reqURL := c.baseURL()

	var body io.Reader = r
	if onProgress != nil && size > 0 {
		body = &progressReader{r: r, onProgress: onProgress, total: size}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return fmt.Errorf("failed to create import request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if size > 0 {
		req.ContentLength = size
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to import snapshot: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("import failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return readStreamEvents(resp.Body, onEvent)
}

// readStreamEvents reads newline-delimited JSON events from r, calling onEvent for each.
// Unknown event types are silently skipped.
func readStreamEvents(r io.Reader, onEvent func(streamEvent)) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip malformed lines defensively
		}
		onEvent(ev)
	}
	return scanner.Err()
}

// progressReader wraps an io.Reader and calls onProgress after each read.
type progressReader struct {
	r          io.Reader
	done       int64
	total      int64
	onProgress func(done, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.done += int64(n)
		total := pr.total
		if total < 0 {
			total = 0
		}
		pr.onProgress(pr.done, total)
	}
	return n, err
}
