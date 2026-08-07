package container

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func infoServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestProbeEmulatorInfoReturnsInfo(t *testing.T) {
	host := infoServer(t, http.StatusOK, `{"version":"4.16.0","edition":"pro","is_docker":false,"uptime":42}`)

	info, err := ProbeEmulatorInfo(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, "4.16.0", info.Version)
	assert.Equal(t, "pro", info.Edition)
	assert.Equal(t, 42, info.Uptime)
}

func TestProbeEmulatorInfoRejectsNon200(t *testing.T) {
	host := infoServer(t, http.StatusServiceUnavailable, `{}`)

	_, err := ProbeEmulatorInfo(context.Background(), host)
	assert.Error(t, err)
}

func TestProbeEmulatorInfoRejectsNonJSON(t *testing.T) {
	host := infoServer(t, http.StatusOK, `<html>not localstack</html>`)

	_, err := ProbeEmulatorInfo(context.Background(), host)
	assert.Error(t, err)
}

func TestProbeEmulatorInfoRejectsEmptyVersion(t *testing.T) {
	// Any JSON object decodes into LocalStackInfo, so a 200 from an unrelated
	// service must not count as a LocalStack instance.
	host := infoServer(t, http.StatusOK, `{"status":"ok"}`)

	_, err := ProbeEmulatorInfo(context.Background(), host)
	assert.Error(t, err)
}

func TestProbeEmulatorInfoUnreachableHost(t *testing.T) {
	_, err := ProbeEmulatorInfo(context.Background(), "127.0.0.1:1")
	assert.Error(t, err)
}
