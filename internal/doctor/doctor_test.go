package doctor

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type fakeEmulatorClient struct {
	version      string
	err          error
	fetchVersion func(context.Context, string) (string, error)
}

func (f fakeEmulatorClient) FetchVersion(ctx context.Context, host string) (string, error) {
	if f.fetchVersion != nil {
		return f.fetchVersion(ctx, host)
	}
	if f.err != nil {
		return "", f.err
	}
	return f.version, nil
}

func (f fakeEmulatorClient) FetchResources(context.Context, string) ([]emulator.Resource, error) {
	return nil, nil
}

func TestRunReturnsSilentErrorWhenCriticalChecksFail(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := Run(context.Background(), nil, nil, sink, Options{
		Config: ConfigState{
			Path:      "/tmp/config.toml",
			Exists:    true,
			LoadError: errors.New("invalid config"),
		},
		Logger:           log.Nop(),
		RuntimeInitError: errors.New("docker init failed"),
	})

	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, out.String(), "Config file")
	assert.Contains(t, out.String(), "FAIL")
	assert.Contains(t, out.String(), "docker init failed")
	assert.Contains(t, out.String(), "Doctor found issues that require attention")
}

func TestRunReportsRunningEmulatorHealth(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().SocketPath().Return("/var/run/docker.sock").AnyTimes()
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().GetBoundPort(gomock.Any(), "localstack-aws", "4566/tcp").Return("4566", nil)

	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := Run(context.Background(), mockRT, fakeEmulatorClient{version: "4.14.1"}, sink, Options{
		Config: ConfigState{
			Path:   "/tmp/config.toml",
			Exists: true,
			Loaded: true,
		},
		Containers: []config.ContainerConfig{{
			Type: config.EmulatorAWS,
			Port: "4566",
		}},
		EnvAuthToken: "test-token",
		Logger:       log.Nop(),
	})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "LocalStack AWS Emulator")
	assert.Contains(t, out.String(), "4.14.1")
	assert.Contains(t, out.String(), "Doctor found no issues")
}

func TestDockerRowReportsTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().SocketPath().Return("/var/run/docker.sock").AnyTimes()
	mockRT.EXPECT().IsHealthy(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, ready := dockerRow(ctx, mockRT, nil)

	require.False(t, ready)
	assert.Equal(t, statusFail, result.status)
	assert.Contains(t, result.detail, "timed out")
}

func TestEmulatorRowsPreserveConfiguredOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").DoAndReturn(func(context.Context, string) (bool, error) {
		time.Sleep(25 * time.Millisecond)
		return true, nil
	})
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-snowflake").Return(true, nil)
	mockRT.EXPECT().GetBoundPort(gomock.Any(), "localstack-aws", "4566/tcp").Return("4566", nil)

	rows := emulatorRows(context.Background(), mockRT, fakeEmulatorClient{
		fetchVersion: func(ctx context.Context, host string) (string, error) {
			if host != "localhost.localstack.cloud:4566" {
				return "", errors.New("unexpected host")
			}
			return "4.14.1", nil
		},
	}, []config.ContainerConfig{
		{Type: config.EmulatorAWS, Port: "4566"},
		{Type: config.EmulatorSnowflake, Port: "4570"},
	}, "", true)

	require.Len(t, rows, 3)
	assert.Equal(t, "LocalStack AWS Emulator", rows[0].check)
	assert.Equal(t, "LocalStack AWS Emulator health", rows[1].check)
	assert.Equal(t, "LocalStack Snowflake Emulator", rows[2].check)
}

func TestRunReportsProgressMessages(t *testing.T) {
	var out bytes.Buffer
	sink := output.NewPlainSink(&out)

	err := Run(context.Background(), nil, nil, sink, Options{
		Config: ConfigState{Path: "/tmp/config.toml", Exists: false},
		Logger: log.Nop(),
	})

	require.Error(t, err)
	assert.Contains(t, out.String(), "Checking configuration")
	assert.Contains(t, out.String(), "Checking Docker runtime")
	assert.Contains(t, out.String(), "Checking authentication")
}
