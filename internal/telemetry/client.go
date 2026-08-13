package telemetry

import (
	"context"
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

	classification caller.Classification

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

// SessionID is the per-process correlation id stamped on every event this client
// emits. It is conveyed to extension processes through the runtime context so an
// extension's own telemetry joins to the lstk invocation that dispatched it.
// Empty when telemetry is disabled, since a disabled client has no session.
func (c *Client) SessionID() string {
	return c.sessionID
}

// machineIDTimeout bounds the Docker daemon lookup behind LoadOrCreateMachineID.
// MachineID runs before an extension execs, where a black-holed DOCKER_HOST would
// otherwise stall dispatch for the OS TCP connect timeout (~75s on macOS); on
// expiry the derivation falls through to the system/generated id.
const machineIDTimeout = 3 * time.Second

// MachineID is the anonymized machine identity stamped on this client's events
// (the salted hash produced by LoadOrCreateMachineID, never a raw Docker or
// system id). It is conveyed to extension processes through the runtime context
// so an extension emitting its own telemetry reports the same machine without
// re-deriving it.
//
// The value is computed lazily on first use and cached for the process; every
// consumer (this accessor and event building via GetEnvironment) goes through
// here. Always empty when telemetry is disabled: a disabled client emits no
// events, so there is no identity to share — and the guard precedes the
// computation, so a disabled client never dials Docker or persists a generated
// id to derive a value it would only mask.
func (c *Client) MachineID(ctx context.Context) string {
	if !c.enabled {
		return ""
	}
	c.machineIDOnce.Do(func() {
		ctx, cancel := context.WithTimeout(ctx, machineIDTimeout)
		defer cancel()
		c.machineID = LoadOrCreateMachineID(ctx)
	})
	return c.machineID
}

func New(endpoint string, disabled bool) *Client {
	if disabled {
		return &Client{enabled: false}
	}
	return newClient(endpoint, caller.New().Classify())
}

func newClient(endpoint string, cl caller.Classification) *Client {
	return &Client{
		enabled:        true,
		sessionID:      uuid.NewString(),
		classification: cl,
		endpoint:       endpoint,
		flushFn:        spawnDetachedFlusher,
		pending:        make([]eventBody, 0, pendingCap),
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

	enriched := make(map[string]any, len(payload)+8)
	for k, v := range payload {
		enriched[k] = v
	}
	enriched["os"] = runtime.GOOS
	enriched["arch"] = runtime.GOARCH
	enriched["caller_type"] = c.classification.CallerType()
	enriched["detection_method"] = c.classification.DetectionMethod()
	enriched["is_ci"] = c.classification.IsCI()
	if c.classification.AgentIdentity != "" {
		enriched["agent_identity"] = c.classification.AgentIdentity
	}
	if c.classification.CIIdentity != "" {
		enriched["ci_identity"] = c.classification.CIIdentity
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
