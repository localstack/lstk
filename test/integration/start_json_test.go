package integration_test

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startJSONData mirrors start's JSON data shape.
type startJSONData struct {
	Emulator       string `json:"emulator"`
	Container      string `json:"container"`
	Endpoint       string `json:"endpoint"`
	Version        string `json:"version"`
	AlreadyRunning bool   `json:"alreadyRunning"`
	Persistence    bool   `json:"persistence"`
}

func TestStartCommandJSON(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start", "--json")
	require.NoError(t, err, "lstk start --json failed: %s", stderr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	assert.Equal(t, "start", envelope.Command)
	assert.Nil(t, envelope.Error)

	var data startJSONData
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	assert.Equal(t, "aws", data.Emulator)
	assert.Equal(t, containerName, data.Container)
	assert.Contains(t, data.Endpoint, "4566")
	assert.NotEmpty(t, data.Version)
	assert.False(t, data.AlreadyRunning)
	assert.False(t, data.Persistence)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should actually be running")
}

func TestStartCommandJSONAlreadyRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token"), "start", "--json")
	require.NoError(t, err, "lstk start --json should succeed when already running: %s", stderr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	assert.Nil(t, envelope.Error)

	var data startJSONData
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	assert.True(t, data.AlreadyRunning)
	assert.Equal(t, containerName, data.Container)
}

func TestStartCommandJSONPortConflict(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	defer func() { _ = ln.Close() }()

	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token"), "start", "--json")
	require.Error(t, err, "expected lstk start --json to fail when port is in use")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "PORT_CONFLICT", envelope.Error.Code)
	assert.Equal(t, "RUNTIME", envelope.Error.Category)
	assert.False(t, envelope.Error.Retryable)
}

func TestStartCommandJSONInvalidLicense(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "invalid-token").With(env.APIEndpoint, mockServer.URL), "start", "--json")
	require.Error(t, err, "expected lstk start --json to fail with an invalid token")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "error", envelope.Status)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "LICENSE_INVALID", envelope.Error.Code)
	assert.Equal(t, "AUTH", envelope.Error.Category)
}
