package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStart_ReturnsEarlyIfRuntimeUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(errors.New("cannot connect to Docker daemon"))
	mockRT.EXPECT().EmitUnhealthyError(gomock.Any(), gomock.Any())

	sink := output.NewPlainSink(io.Discard)

	err := Start(context.Background(), mockRT, sink, StartOptions{Logger: log.Nop()}, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime not healthy")
	assert.True(t, output.IsSilent(err), "error should be silent since it was already emitted")
}

func TestEmitPostStartPointers_WithWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, "localhost.localstack.cloud:4566", "https://app.localstack.cloud/")

	got := out.String()
	assert.Contains(t, got, "• Endpoint: localhost.localstack.cloud:4566\n")
	assert.Contains(t, got, "• Web app: https://app.localstack.cloud\n")
	assert.Contains(t, got, "> Tip:")
}

func TestEmitPostStartPointers_WithoutWebApp(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	emitPostStartPointers(sink, "127.0.0.1:4566", "")

	got := out.String()
	assert.Contains(t, got, "• Endpoint: 127.0.0.1:4566\n")
	assert.Contains(t, got, "> Tip:")
}

func TestServicePortRange_ReturnsExpectedPorts(t *testing.T) {
	ports := servicePortRange()

	require.Len(t, ports, 51)
	assert.Equal(t, "443", ports[0].ContainerPort)
	assert.Equal(t, "443", ports[0].HostPort)
	assert.Equal(t, "4510", ports[1].ContainerPort)
	assert.Equal(t, "4510", ports[1].HostPort)
	assert.Equal(t, "4559", ports[50].ContainerPort)
	assert.Equal(t, "4559", ports[50].HostPort)
}

func TestFilterHostEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{"CI is included", "CI=true", true},
		{"LOCALSTACK_DISABLE_EVENTS is included", "LOCALSTACK_DISABLE_EVENTS=1", true},
		{"LOCALSTACK_HOST is included", "LOCALSTACK_HOST=0.0.0.0", true},
		{"LOCALSTACK_AUTH_TOKEN is excluded", "LOCALSTACK_AUTH_TOKEN=secret", false},
		{"PATH is excluded", "PATH=/usr/bin", false},
		{"HOME is excluded", "HOME=/root", false},
		{"LOCALSTACK_BUILD_VERSION is included", "LOCALSTACK_BUILD_VERSION=3.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.HasPrefix(tt.env, "CI=") || (strings.HasPrefix(tt.env, "LOCALSTACK_") && !strings.HasPrefix(tt.env, "LOCALSTACK_AUTH_TOKEN="))
			assert.Equal(t, tt.expected, got)
		})
	}
}
