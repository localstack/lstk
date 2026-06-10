package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/caller"
)

func TestClose_IsIdempotent(t *testing.T) {
	t.Parallel()
	c := New("http://localhost", false)
	c.Close()
	// Second call must not panic.
	c.Close()
}

func TestEmit_EnrichesPayloadIntoPending(t *testing.T) {
	t.Parallel()
	c := newClient("http://localhost", caller.Classification{Type: caller.TypeHuman, Method: caller.MethodTTY})
	c.Emit(context.Background(), "cli_cmd", map[string]any{"cmd": "lstk start"})

	require.Len(t, c.pending, 1)
	ev := c.pending[0]
	assert.Equal(t, "cli_cmd", ev.Name)
	assert.Equal(t, c.sessionID, ev.Metadata.SessionID)
	_, err := time.Parse("2006-01-02 15:04:05.000000", ev.Metadata.ClientTime)
	assert.NoError(t, err)

	payload, ok := ev.Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "lstk start", payload["cmd"])
	assert.Equal(t, runtime.GOOS, payload["os"])
	assert.Equal(t, runtime.GOARCH, payload["arch"])
	_, isCI := os.LookupEnv("CI")
	assert.Equal(t, isCI, payload["is_ci"])
	assert.Equal(t, "human", payload["caller_type"])
	assert.Equal(t, caller.MethodTTY, payload["detection_method"])
	assert.NotContains(t, payload, "caller_identity")
}

func TestEmit_IncludesCallerIdentityWhenPresent(t *testing.T) {
	t.Parallel()
	c := newClient("http://localhost", caller.Classification{
		Type:     caller.TypeAgent,
		Identity: "claude-code",
		Method:   caller.MethodAgentEnv,
	})
	c.Emit(context.Background(), "cli_cmd", map[string]any{"cmd": "lstk start"})

	require.Len(t, c.pending, 1)
	payload, ok := c.pending[0].Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "agent", payload["caller_type"])
	assert.Equal(t, caller.MethodAgentEnv, payload["detection_method"])
	assert.Equal(t, "claude-code", payload["caller_identity"])
}

func TestNew_DisabledClientHasNoClassification(t *testing.T) {
	t.Parallel()
	c := New("http://localhost", true)
	assert.Empty(t, c.callerType)
	assert.Empty(t, c.callerIdentity)
	assert.Empty(t, c.detectionMethod)
}

func TestEmit_DropsOldestWhenFull(t *testing.T) {
	t.Parallel()
	c := New("http://localhost", false)
	for i := 0; i < pendingCap+5; i++ {
		c.Emit(context.Background(), "cli_cmd", map[string]any{"i": i})
	}
	assert.Len(t, c.pending, pendingCap, "pending must be capped")
	// The oldest 5 events should have been dropped.
	first := c.pending[0].Payload.(map[string]any)
	assert.EqualValues(t, 5, first["i"])
}

func TestEmit_NoopWhenDisabled(t *testing.T) {
	t.Parallel()
	c := New("http://localhost", true)
	c.Emit(context.Background(), "cli_cmd", map[string]any{"cmd": "lstk start"})
	assert.Empty(t, c.pending)
}

func TestClose_HandsOffPendingEventsToFlusher(t *testing.T) {
	t.Parallel()
	var (
		gotEPs []string
		gotEvs [][]eventBody
	)
	c := New("http://example.test/events", false)
	c.flushFn = func(_ context.Context, endpoint string, events []eventBody) {
		gotEPs = append(gotEPs, endpoint)
		gotEvs = append(gotEvs, events)
	}

	c.Emit(context.Background(), "cli_cmd", map[string]any{"cmd": "lstk start"})
	c.Close()

	require.Len(t, gotEPs, 1)
	assert.Equal(t, "http://example.test/events", gotEPs[0])
	require.Len(t, gotEvs, 1)
	require.Len(t, gotEvs[0], 1)
	assert.Equal(t, "cli_cmd", gotEvs[0][0].Name)

	// Second close must not spawn again.
	c.Close()
	assert.Len(t, gotEPs, 1)
}

func TestClose_SkipsSpawnWhenNoPending(t *testing.T) {
	t.Parallel()
	var called bool
	c := New("http://example.test/events", false)
	c.flushFn = func(context.Context, string, []eventBody) { called = true }

	c.Close()
	assert.False(t, called, "the flusher must not run when there are no events")
}

func TestRunFlush_PostsEachEventWithCorrectPayloadAndHeaders(t *testing.T) {
	t.Parallel()
	type captured struct {
		event  map[string]any
		header http.Header
	}
	ch := make(chan captured, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var req struct {
			Events []map[string]any `json:"events"`
		}
		assert.NoError(t, json.Unmarshal(body, &req))
		for _, ev := range req.Events {
			ch <- captured{event: ev, header: r.Header.Clone()}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Build the input the way the parent process would.
	c := New(srv.URL, false)
	c.Emit(context.Background(), "cli_cmd", map[string]any{"cmd": "lstk start"})
	c.Emit(context.Background(), "cli_lifecycle", map[string]any{"event_type": "stop"})

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, ev := range c.pending {
		require.NoError(t, enc.Encode(ev))
	}

	require.NoError(t, RunFlush(context.Background(), srv.URL, &buf))

	got := drain(ch, 2, 2*time.Second)
	require.Len(t, got, 2)

	byName := map[string]captured{}
	for _, c := range got {
		byName[c.event["name"].(string)] = c
	}
	assert.Contains(t, byName, "cli_cmd")
	assert.Contains(t, byName, "cli_lifecycle")
	assert.True(t, strings.HasPrefix(byName["cli_cmd"].header.Get("User-Agent"), "localstack lstk/"))
}

func drain[T any](ch <-chan T, n int, timeout time.Duration) []T {
	out := make([]T, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case v := <-ch:
			out = append(out, v)
		case <-deadline:
			return out
		}
	}
	return out
}
