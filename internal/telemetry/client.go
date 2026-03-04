package telemetry

import (
	"bytes"
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
	return fmt.Sprintf("localstack lstk/%s (%s; %s)", version.Version(), runtime.GOOS, runtime.GOARCH)
}

type Client struct {
	enabled    bool
	sessionID  string
	machineID  string
	httpClient *http.Client
	endpoint   string
	wg         sync.WaitGroup
}

func New(endpoint string, disabled bool) *Client {
	if disabled {
		return &Client{enabled: false}
	}
	return &Client{
		enabled:   true,
		sessionID: uuid.NewString(),
		machineID: LoadOrCreateMachineID(),
		// http.Client has no default timeout (zero means none). Without one, a
		// slow or unreachable endpoint would block the goroutine until process
		// exit — which matters for long-running commands like `lstk logs --follow`.
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		endpoint: endpoint,
	}
}

type requestBody struct {
	Events []eventBody `json:"events"`
}

type eventBody struct {
	Name     string        `json:"name"`
	Metadata eventMetadata `json:"metadata"`
	Payload  any           `json:"payload"`
}

type eventMetadata struct {
	ClientTime string `json:"client_time"`
	SessionID  string `json:"session_id"`
}

func (c *Client) Track(name string, payload map[string]any) {
	if !c.enabled {
		return
	}

	enriched := make(map[string]any, len(payload)+6)
	for k, v := range payload {
		enriched[k] = v
	}
	enriched["version"] = version.Version()
	enriched["os"] = runtime.GOOS
	enriched["arch"] = runtime.GOARCH
	enriched["is_ci"] = os.Getenv("CI") != ""
	if c.machineID != "" {
		enriched["machine_id"] = c.machineID
	}

	body := eventBody{
		Name: name,
		Metadata: eventMetadata{
			ClientTime: time.Now().UTC().Format("2006-01-02 15:04:05.000000"),
			SessionID:  c.sessionID,
		},
		Payload: enriched,
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		data, err := json.Marshal(requestBody{Events: []eventBody{body}})
		if err != nil {
			return
		}

		req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(data))
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
	}()
}

// Flush blocks until all in-flight Track goroutines have completed. Call it
// before process exit to avoid dropping telemetry events. It returns quickly
// when no events are pending, and is bounded by the HTTP client's timeout in
// the worst case.
func (c *Client) Flush() {
	c.wg.Wait()
}

