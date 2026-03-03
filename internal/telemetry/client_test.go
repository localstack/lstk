package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrack_SendsCorrectPayloadAndHeaders(t *testing.T) {
	type captured struct {
		event  map[string]any
		header http.Header
	}
	ch := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req struct {
			Events []map[string]any `json:"events"`
		}
		assert.NoError(t, json.Unmarshal(body, &req))
		assert.Len(t, req.Events, 1)

		if len(req.Events) == 1 {
			ch <- captured{event: req.Events[0], header: r.Header.Clone()}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, false)
	c.Track("cli_cmd", map[string]any{"cmd": "lstk start", "params": []string{}})
	c.Flush()

	var got captured
	select {
	case got = <-ch:
	default:
		t.Fatal("no telemetry event received")
	}

	assert.Equal(t, "cli_cmd", got.event["name"])

	metadata, ok := got.event["metadata"].(map[string]any)
	require.True(t, ok, "metadata should be an object")
	assert.Equal(t, c.sessionID, metadata["session_id"])
	_, err := time.Parse("2006-01-02 15:04:05.000000", metadata["client_time"].(string))
	assert.NoError(t, err, "client_time should match expected format")
	assert.Nil(t, metadata["version"], "version should be in payload, not metadata")
	assert.Nil(t, metadata["machine_id"], "machine_id should be in payload, not metadata")

	payload, ok := got.event["payload"].(map[string]any)
	require.True(t, ok, "payload should be an object")
	assert.Equal(t, "lstk start", payload["cmd"])
	assert.Equal(t, version.Version(), payload["version"])
	assert.Equal(t, runtime.GOOS, payload["os"])
	assert.Equal(t, runtime.GOARCH, payload["arch"])
	assert.Equal(t, os.Getenv("CI") != "", payload["is_ci"])

	assert.Equal(t, "lstk/v2", got.header.Get("X-Client"))
}
