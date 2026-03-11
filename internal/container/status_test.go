package container

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStatus_NotRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	sink := output.NewPlainSink(io.Discard)

	err := Status(context.Background(), mockRT, containers, "", sink)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, err.Error(), "is not running")
}

func TestStatus_IsRunningError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, fmt.Errorf("docker unavailable"))

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	sink := output.NewPlainSink(io.Discard)

	err := Status(context.Background(), mockRT, containers, "", sink)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker unavailable")
}

func TestStatus_RunningWithResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1"}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"AWS::S3::Bucket": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-bucket"}]}`)
			_, _ = fmt.Fprintln(w, `{"AWS::Lambda::Function": [{"region_name": "us-east-1", "account_id": "000000000000", "id": "my-func"}]}`)
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().ContainerStartedAt(gomock.Any(), "localstack-aws").Return(time.Now().Add(-5*time.Minute), nil)

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	var buf strings.Builder
	sink := output.NewPlainSink(&buf)

	err := Status(context.Background(), mockRT, containers, host, sink)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "is running")
	assert.Contains(t, out, "4.14.1")
	assert.Contains(t, out, "S3")
	assert.Contains(t, out, "my-bucket")
	assert.Contains(t, out, "Lambda")
	assert.Contains(t, out, "my-func")
	assert.Contains(t, out, "2 resources")
	assert.Contains(t, out, "2 services")
}

func TestStatus_RunningNoResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1"}`)
		case "/_localstack/resources":
			w.Header().Set("Content-Type", "application/x-ndjson")
		}
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")

	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().ContainerStartedAt(gomock.Any(), "localstack-aws").Return(time.Time{}, fmt.Errorf("not found"))

	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	var buf strings.Builder
	sink := output.NewPlainSink(&buf)

	err := Status(context.Background(), mockRT, containers, host, sink)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "is running")
	assert.Contains(t, out, "No resources deployed")
}

func TestStatus_MultipleContainers_StopsAtFirstNotRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)

	containers := []config.ContainerConfig{
		{Type: config.EmulatorAWS},
		{Type: config.EmulatorSnowflake},
	}
	sink := output.NewPlainSink(io.Discard)

	err := Status(context.Background(), mockRT, containers, "", sink)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
}
