package container

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const offlineImage = "registry.example.com/localstack-enterprise:2026.8.4"

func TestNeedsAuthToken(t *testing.T) {
	tests := []struct {
		name     string
		env      []string
		envErr   error
		expected bool
	}{
		{name: "bundled license", env: []string{"PATH=/bin", "LOCALSTACK_AUTH_TOKEN_OVERRIDE=ls-bundled"}, expected: false},
		{name: "regular image", env: []string{"PATH=/bin", "LOCALSTACK_BUILD_VERSION=2026.8.4"}, expected: true},
		{name: "blank override", env: []string{"LOCALSTACK_AUTH_TOKEN_OVERRIDE="}, expected: true},
		{name: "image missing or inspect failed", envErr: errors.New("no such image"), expected: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRT := runtime.NewMockRuntime(ctrl)
			mockRT.EXPECT().ImageEnv(gomock.Any(), offlineImage).Return(tc.env, tc.envErr)

			opts := StartOptions{
				Containers:    []config.ContainerConfig{{Type: config.EmulatorAWS, Port: "4566"}},
				ImageOverride: offlineImage,
			}
			assert.Equal(t, tc.expected, NeedsAuthToken(context.Background(), mockRT, opts))
		})
	}
}

func TestNeedsAuthToken_UsesConfiguredImageWithoutOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().ImageEnv(gomock.Any(), "localstack-enterprise:latest").Return([]string{"LOCALSTACK_AUTH_TOKEN_OVERRIDE=ls-bundled"}, nil)

	opts := StartOptions{Containers: []config.ContainerConfig{{Type: config.EmulatorAWS, CustomImage: "localstack-enterprise"}}}
	assert.False(t, NeedsAuthToken(context.Background(), mockRT, opts))
}

func TestNeedsAuthToken_NoContainers(t *testing.T) {
	ctrl := gomock.NewController(t)
	assert.True(t, NeedsAuthToken(context.Background(), runtime.NewMockRuntime(ctrl), StartOptions{}))
}

func TestPullImages_SkipsPullForBundledLicenseOnFloatingTag(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	c := runtime.ContainerConfig{
		Image:          "localstack-enterprise:latest",
		Name:           "localstack-aws",
		EmulatorType:   config.EmulatorAWS,
		Tag:            "latest",
		BundledLicense: true,
	}
	mockRT.EXPECT().Remove(gomock.Any(), c.Name).Return(nil)
	mockRT.EXPECT().ImageExists(gomock.Any(), c.Image).Return(true, nil)
	// No PullImage expected.

	var out bytes.Buffer
	pulled, err := pullImages(context.Background(), mockRT, output.NewPlainSink(&out), telemetry.New("", true), []runtime.ContainerConfig{c}, true)

	require.NoError(t, err)
	assert.False(t, pulled[c.Name])
	assert.Contains(t, out.String(), "Using local image localstack-enterprise:latest")
}

func TestLicenseValidation_SkippedForBundledLicense(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)

	var licenseHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&licenseHits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	opts := StartOptions{
		PlatformClient: api.NewPlatformClient(srv.URL, log.Nop()),
		Logger:         log.Nop(),
		Telemetry:      telemetry.New("", true),
	}
	licenseFile := filepath.Join(t.TempDir(), "license.json")
	sink := output.NewPlainSink(io.Discard)

	pinned := runtime.ContainerConfig{Image: offlineImage, Name: "localstack-aws-2026.8.4", EmulatorType: config.EmulatorAWS, ProductName: "localstack-pro", Tag: "2026.8.4", BundledLicense: true}
	floating := runtime.ContainerConfig{Image: "localstack-enterprise:latest", Name: "localstack-aws", EmulatorType: config.EmulatorAWS, ProductName: "localstack-pro", Tag: "latest", BundledLicense: true}

	postPull, refreshed, err := tryPrePullLicenseValidation(context.Background(), mockRT, sink, opts, []runtime.ContainerConfig{pinned, floating}, "", licenseFile, true)
	require.NoError(t, err)
	assert.False(t, refreshed)
	require.Len(t, postPull, 1, "the floating tag still goes through post-pull version resolution")

	mockRT.EXPECT().GetImageVersion(gomock.Any(), floating.Image).Return("2026.8.4", nil)
	version, refreshed, err := validateLicensesFromImages(context.Background(), mockRT, sink, opts, postPull, "", licenseFile)
	require.NoError(t, err)
	assert.False(t, refreshed)
	assert.Equal(t, "2026.8.4", version, "the version is still resolved from the local image")

	assert.Equal(t, int32(0), atomic.LoadInt32(&licenseHits), "a bundled license is never checked against the platform")
}

func TestMountCachedLicense_SkipsBundledLicense(t *testing.T) {
	licenseFile := filepath.Join(t.TempDir(), "license.json")
	require.NoError(t, os.WriteFile(licenseFile, []byte("{}"), 0600))

	containers := []runtime.ContainerConfig{{Name: "localstack-aws", BundledLicense: true}}
	assert.False(t, mountCachedLicense(containers, licenseFile), "nothing was mounted, so there is nothing to retry")
	assert.Empty(t, containers[0].Binds)
}

func TestStartOnce_FailsClosedWithoutTokenForNonBundledImage(t *testing.T) {
	// An empty token must not reach the platform once the image no longer qualifies.
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().ImageEnv(gomock.Any(), offlineImage).Return(nil, errors.New("no such image"))

	opts := StartOptions{
		Containers:    []config.ContainerConfig{{Type: config.EmulatorAWS, Port: "4566"}},
		ImageOverride: offlineImage,
		Logger:        log.Nop(),
		Telemetry:     telemetry.New("", true),
	}
	var out bytes.Buffer
	_, err := startOnce(context.Background(), mockRT, output.NewPlainSink(&out), opts, false, "", filepath.Join(t.TempDir(), "license.json"), false)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, out.String(), "authentication required")
}
