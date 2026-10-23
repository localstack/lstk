package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDaemon answers the few Docker API calls a container start, stop and inspection make.
type fakeDaemon struct {
	mu        sync.Mutex
	created   map[string]any
	inspect   string
	stopQuery url.Values
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", "1.51")
	switch path := r.URL.Path; {
	case strings.HasSuffix(path, "/_ping"):
		w.WriteHeader(http.StatusOK)
	case strings.HasSuffix(path, "/containers/create"):
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		_ = json.Unmarshal(body, &f.created)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"Id":"cid-1","Warnings":[]}`)
	case strings.HasSuffix(path, "/wait"):
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"StatusCode":0}`)
	case strings.HasSuffix(path, "/start"):
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(path, "/stop"):
		f.mu.Lock()
		f.stopQuery = r.URL.Query()
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(path, "/json"):
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, f.inspect)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeDaemonRuntime(t *testing.T, daemon *fakeDaemon) *DockerRuntime {
	t.Helper()
	srv := httptest.NewServer(daemon)
	t.Cleanup(srv.Close)
	cli, err := client.New(client.WithHost("tcp://" + strings.TrimPrefix(srv.URL, "http://")))
	require.NoError(t, err)
	return &DockerRuntime{client: cli}
}

func TestStart_GivesTheContainerItsStopTimeout(t *testing.T) {
	daemon := &fakeDaemon{}
	rt := newFakeDaemonRuntime(t, daemon)

	_, _, err := rt.Start(context.Background(), ContainerConfig{
		Image: "localstack/localstack-azure", Name: "localstack-azure", Port: "4566", ContainerPort: "4566/tcp", StopTimeout: 65,
	})
	require.NoError(t, err)

	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	assert.EqualValues(t, 65, daemon.created["StopTimeout"])
}

func TestStart_LeavesDockersDefaultStopTimeoutWhenNoneIsSet(t *testing.T) {
	daemon := &fakeDaemon{}
	rt := newFakeDaemonRuntime(t, daemon)

	_, _, err := rt.Start(context.Background(), ContainerConfig{
		Image: "localstack/localstack-pro", Name: "localstack-aws", Port: "4566", ContainerPort: "4566/tcp",
	})
	require.NoError(t, err)

	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	assert.Nil(t, daemon.created["StopTimeout"])
}

func TestInspectBrief_ReportsTheContainersStopTimeout(t *testing.T) {
	rt := newFakeDaemonRuntime(t, &fakeDaemon{
		inspect: `{"Id":"cid-1","State":{"Status":"running","Running":true},"Config":{"Image":"localstack/localstack-azure","StopTimeout":65,"Labels":{}},"HostConfig":{"AutoRemove":true}}`,
	})

	brief, err := rt.InspectBrief(context.Background(), "localstack-azure")
	require.NoError(t, err)

	require.NotNil(t, brief.StopTimeout)
	assert.Equal(t, 65, *brief.StopTimeout)
}

func TestInspectBrief_ReportsNoStopTimeoutWhenTheContainerHasNone(t *testing.T) {
	rt := newFakeDaemonRuntime(t, &fakeDaemon{
		inspect: `{"Id":"cid-1","State":{"Status":"running","Running":true},"Config":{"Image":"localstack/localstack-pro","Labels":{}},"HostConfig":{"AutoRemove":true}}`,
	})

	brief, err := rt.InspectBrief(context.Background(), "localstack-aws")
	require.NoError(t, err)

	assert.Nil(t, brief.StopTimeout)
}

func TestStop_PassesTheStopTimeoutToDocker(t *testing.T) {
	daemon := &fakeDaemon{}
	rt := newFakeDaemonRuntime(t, daemon)
	timeout := 65

	require.NoError(t, rt.Stop(context.Background(), "localstack-azure", &timeout))

	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	assert.Equal(t, "65", daemon.stopQuery.Get("t"))
}

func TestStop_LeavesTheContainersOwnStopTimeoutWithoutOne(t *testing.T) {
	daemon := &fakeDaemon{}
	rt := newFakeDaemonRuntime(t, daemon)

	require.NoError(t, rt.Stop(context.Background(), "localstack-aws", nil))

	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	require.NotNil(t, daemon.stopQuery, "the stop must reach Docker")
	assert.False(t, daemon.stopQuery.Has("t"))
}
