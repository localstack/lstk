package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/localstack/lstk/internal/version"
)

func userAgent() string {
	return fmt.Sprintf("localstack lstk/%s", version.Version())
}

type Client struct {
	enabled   bool
	sessionID string
	machineID string

	httpClient *http.Client
	endpoint   string

	events    chan eventBody
	done      chan struct{}
	closeOnce sync.Once
}

func New(endpoint string, disabled bool) *Client {
	if disabled {
		return &Client{enabled: false}
	}
	c := &Client{
		enabled:   true,
		sessionID: uuid.NewString(),
		machineID: LoadOrCreateMachineID(),
		// http.Client has no default timeout (zero means none). Without one, a
		// slow or unreachable endpoint would block the worker goroutine.
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		endpoint: endpoint,
		events:   make(chan eventBody, 64),
		done:     make(chan struct{}),
	}
	go c.worker()
	return c
}

type requestBody struct {
	Events []eventBody `json:"events"`
}

type eventBody struct {
	ctx      context.Context // not serialized; carries context to the worker
	Name     string          `json:"name"`
	Metadata eventMetadata   `json:"metadata"`
	Payload  any             `json:"payload"`
}

type eventMetadata struct {
	ClientTime string `json:"client_time"`
	SessionID  string `json:"session_id"`
}

func (c *Client) Emit(ctx context.Context, name string, payload map[string]any) {
	if !c.enabled {
		return
	}

	enriched := make(map[string]any, len(payload)+5)
	for k, v := range payload {
		enriched[k] = v
	}
	enriched["os"] = runtime.GOOS
	enriched["arch"] = runtime.GOARCH
	_, enriched["is_ci"] = os.LookupEnv("CI")
	if c.machineID != "" {
		enriched["machine_id"] = c.machineID
	}

	body := eventBody{
		ctx:  context.WithoutCancel(ctx),
		Name: name,
		Metadata: eventMetadata{
			ClientTime: time.Now().UTC().Format("2006-01-02 15:04:05.000000"),
			SessionID:  c.sessionID,
		},
		Payload: enriched,
	}
	select {
	case c.events <- body:
	default:
	}
}

func (c *Client) worker() {
	defer close(c.done)
	for body := range c.events {
		c.send(body)
	}
}

func (c *Client) send(body eventBody) {
	data, err := json.Marshal(requestBody{Events: []eventBody{body}})
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(body.ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Close stops accepting new events, drains the event buffer, and blocks until
// all pending HTTP requests have completed. Call it before process exit to
// avoid dropping telemetry events.
func (c *Client) Close() {
	if !c.enabled {
		return
	}
	c.closeOnce.Do(func() {
		close(c.events)
		<-c.done
	})
}
