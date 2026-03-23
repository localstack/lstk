package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestNeedsImagePull_AlwaysPolicy(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// HasImage should not be called for "always"
	needs, err := needsImagePull(context.Background(), mockRT, "always", "test/image:latest")
	require.NoError(t, err)
	assert.True(t, needs)
}

func TestNeedsImagePull_NeverPolicy_ImageExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().HasImage(gomock.Any(), "test/image:latest").Return(true, nil)

	needs, err := needsImagePull(context.Background(), mockRT, "never", "test/image:latest")
	require.NoError(t, err)
	assert.False(t, needs)
}

func TestNeedsImagePull_NeverPolicy_ImageMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().HasImage(gomock.Any(), "test/image:latest").Return(false, nil)

	_, err := needsImagePull(context.Background(), mockRT, "never", "test/image:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found locally")
}

func TestNeedsImagePull_AutoPolicy_NoCacheImageMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	// No cache file exists for this image, so shouldPull returns true
	needs, err := needsImagePull(context.Background(), mockRT, "auto", "uncached/image:latest")
	require.NoError(t, err)
	assert.True(t, needs)
}

func TestNeedsImagePull_AutoPolicy_FreshCacheImageExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	image := "test-auto-fresh/image:latest"
	require.NoError(t, recordPull(image))
	t.Cleanup(func() {
		dir, _ := pullCacheDir()
		_ = os.Remove(filepath.Join(dir, sanitizeImageName(image)))
	})

	mockRT.EXPECT().HasImage(gomock.Any(), image).Return(true, nil)

	needs, err := needsImagePull(context.Background(), mockRT, "auto", image)
	require.NoError(t, err)
	assert.False(t, needs)
}

func TestNeedsImagePull_AutoPolicy_FreshCacheImageRemoved(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	image := "test-auto-removed/image:latest"
	require.NoError(t, recordPull(image))
	t.Cleanup(func() {
		dir, _ := pullCacheDir()
		_ = os.Remove(filepath.Join(dir, sanitizeImageName(image)))
	})

	mockRT.EXPECT().HasImage(gomock.Any(), image).Return(false, nil)

	needs, err := needsImagePull(context.Background(), mockRT, "auto", image)
	require.NoError(t, err)
	assert.True(t, needs, "should pull when image was removed externally despite fresh cache")
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
