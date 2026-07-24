package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCreateSendsExpectedPayloadAndAuth(t *testing.T) {
	var gotAuth string
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/compute/instances", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotPayload))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instance_name":"dev","status":"pending"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	body, err := client.Create(context.Background(), CreateOptions{
		Name:            "dev",
		LifetimeMinutes: 90,
		EnvVars: map[string]string{
			"DEBUG": "1",
		},
	})
	require.NoError(t, err)

	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":test-token"))
	assert.Equal(t, expectedAuth, gotAuth)
	assert.Equal(t, "dev", gotPayload["instance_name"])
	assert.Equal(t, float64(90), gotPayload["lifetime"])
	assert.Equal(t, map[string]any{"DEBUG": "1"}, gotPayload["env_vars"])
	assert.JSONEq(t, `{"instance_name":"dev","status":"pending"}`, string(body))
}

func TestClientDescribeMapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/compute/instances/missing", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	_, err := client.Describe(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestClientDescribeReturnsTypedInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/compute/instances/my-box", r.URL.Path)
		_, _ = w.Write([]byte(`{"instance_name":"my-box","status":"running","endpoint_url":"https://my-box.localstack.cloud","expiry_time":"2026-05-05T12:00:00Z"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	inst, err := client.Describe(context.Background(), "my-box")
	require.NoError(t, err)
	assert.Equal(t, "my-box", inst.Name)
	assert.Equal(t, "running", inst.Status)
	assert.Equal(t, "https://my-box.localstack.cloud", inst.Endpoint)
	assert.Equal(t, "2026-05-05T12:00:00Z", inst.Expires)
}

func TestClientListReturnsTypedInstances(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/compute/instances", r.URL.Path)
		_, _ = w.Write([]byte(`{"instances":[{"instance_name":"a","status":"running","endpoint_url":"http://a.local","expiry_time":"2026-05-05T10:00:00Z"},{"instance_name":"b","status":"stopped","endpoint_url":"","expiry_time":""}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	instances, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, instances, 2)
	assert.Equal(t, "a", instances[0].Name)
	assert.Equal(t, "running", instances[0].Status)
	assert.Equal(t, "http://a.local", instances[0].Endpoint)
	assert.Equal(t, "2026-05-05T10:00:00Z", instances[0].Expires)
	assert.Equal(t, "b", instances[1].Name)
}

func TestClientLogsReturnsTypedLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/compute/instances/my-box/logs", r.URL.Path)
		_, _ = w.Write([]byte(`[{"content":"line one"},{"content":"line two"}]`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	lines, err := client.Logs(context.Background(), "my-box")
	require.NoError(t, err)
	assert.Equal(t, []string{"line one", "line two"}, lines)
}

func TestClientResetStateDoesNotSendPlatformAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/_localstack/state/reset", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient("https://api.localstack.cloud", "test-token", log.Nop())
	require.NoError(t, client.ResetState(context.Background(), srv.URL))
	assert.Empty(t, gotAuth)
}

func TestClientWaitForDeletionRetriesTransientErrors(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/compute/instances/to-delete", r.URL.Path)
		attempts++
		switch attempts {
		case 1, 2:
			w.WriteHeader(http.StatusInternalServerError)
		case 3:
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected attempt %d", attempts)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	sink := output.NewPlainSink(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.WaitForDeletion(ctx, sink, "to-delete", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestClientWaitForDeletionReturnsErrorOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"instance_name":"stuck","status":"running"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-token", log.Nop())
	sink := output.NewPlainSink(nil)
	ctx := context.Background()

	err := client.WaitForDeletion(ctx, sink, "stuck", 100*time.Millisecond)
	require.Error(t, err)
	assert.ErrorContains(t, err, "timed out")
}
