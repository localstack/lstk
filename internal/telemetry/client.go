package telemetry

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/localstack/lstk/internal/caller"
)

// pendingCap bounds in-memory events; on overflow the oldest is dropped.
const pendingCap = 64

type Client struct {
	enabled   bool
	sessionID string
	machineID string
	authToken string

	callerType      string
	callerIdentity  string
	detectionMethod string

	endpoint string
	flushFn  func(ctx context.Context, endpoint string, events []eventBody)

	mu            sync.Mutex
	pending       []eventBody
	traceCtx      context.Context // last Emit ctx; carries the command span for trace propagation
	closeOnce     sync.Once
	machineIDOnce sync.Once
}

// SetAuthToken stores the resolved auth token for inclusion in telemetry events.
// Call this once the token is known (e.g. after keyring resolution or interactive login).
func (c *Client) SetAuthToken(token string) {
	c.authToken = token
}

func New(endpoint string, disabled bool) *Client {
	if disabled {
		return &Client{enabled: false}
	}
	return newClient(endpoint, caller.New().Classify())
}

func newClient(endpoint string, cl caller.Classification) *Client {
	return &Client{
		enabled:         true,
		sessionID:       uuid.NewString(),
		callerType:      string(cl.Type),
		callerIdentity:  cl.Identity,
		detectionMethod: cl.Method,
		endpoint:        endpoint,
		flushFn:         spawnDetachedFlusher,
		pending:         make([]eventBody, 0, pendingCap),
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
	enriched["caller_type"] = c.callerType
	enriched["detection_method"] = c.detectionMethod
	if c.callerIdentity != "" {
		enriched["caller_identity"] = c.callerIdentity
	}
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

	c.mu.Lock()
	if len(c.pending) >= pendingCap {
		c.pending = c.pending[1:]
	}
	c.pending = append(c.pending, body)
	c.traceCtx = context.WithoutCancel(ctx)
	c.mu.Unlock()
}

// Close hands pending events to a detached subprocess and returns immediately,
// so analytics endpoint latency never delays command exit.
func (c *Client) Close() {
	if !c.enabled {
		return
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		pending := c.pending
		traceCtx := c.traceCtx
		c.pending = nil
		c.mu.Unlock()
		if len(pending) == 0 {
			return
		}
		if traceCtx == nil {
			traceCtx = context.Background()
		}
		c.flushFn(traceCtx, c.endpoint, pending)
	})
}
