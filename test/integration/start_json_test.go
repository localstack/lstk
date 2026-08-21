package integration_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonStartData mirrors `start`'s data shape in docs/structured-output.md.
// The shared envelope harness leaves Data raw on purpose.
type jsonStartData struct {
	Emulators      []jsonStartedEmulator `json:"emulators"`
	SnapshotLoaded *jsonSnapshotLoaded   `json:"snapshotLoaded"`
}

type jsonStartedEmulator struct {
	Type           string `json:"type"`
	Name           string `json:"name"`
	Host           string `json:"host"`
	Version        string `json:"version"`
	AlreadyRunning bool   `json:"alreadyRunning"`
	Persist        bool   `json:"persist"`
}

type jsonSnapshotLoaded struct {
	Source   string   `json:"source"`
	Services []string `json:"services"`
}

func decodeStartData(t *testing.T, envelope jsonEnvelope) jsonStartData {
	t.Helper()
	var data jsonStartData
	require.NoError(t, json.Unmarshal(envelope.Data, &data), "data should decode into start's documented shape: %s", envelope.Data)
	return data
}

// The flag conflict resolves before any Docker interaction, so this pins the
// envelope wiring (annotation + sink) without a daemon.
func TestStartJSONRejectsConflictingSnapshotFlags(t *testing.T) {
	t.Parallel()

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"start", "--json", "--snapshot", "pod:baseline", "--no-snapshot")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr, "the envelope is the only output; no plain-text fallback")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	assert.Equal(t, "error", envelope.Status)
	assert.JSONEq(t, "null", string(envelope.Data), "data must be null when status is error")
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "VALIDATION_ERROR", envelope.Error.Code)
	assert.Equal(t, "USAGE", envelope.Error.Category)
	assert.False(t, envelope.Error.Retryable)
}

// Replaces the unreachable "two configured types report two entries" scenario:
// checkSingleContainer runs first, so this is a config error, not a 2-entry array.
func TestStartJSONMultipleContainersRendersConfigInvalid(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile,
		[]byte("[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4567\"\n"), 0644))

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "start", "--json")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr, "the envelope is the only output; no plain-text fallback")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "CONFIG_INVALID", envelope.Error.Code)
	assert.Equal(t, "CONFIG", envelope.Error.Category)
	summary, _ := envelope.Error.Details["summary"].(string)
	assert.Contains(t, summary, "only one is supported at a time",
		"the diagnostic plain text shows must survive into details")
}

// Pins the retryable classification for an unreachable runtime.
func TestStartJSONRuntimeUnavailable(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"), 0644))

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "start", "--json")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr, "the envelope is the only output; no plain-text fallback")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "RUNTIME_UNAVAILABLE", envelope.Error.Code)
	assert.Equal(t, "RUNTIME", envelope.Error.Category)
	assert.True(t, envelope.Error.Retryable, "an unreachable runtime is worth retrying")
}

// `lstk --json` runs start's behavior, so its envelope must name `start`
// rather than be rejected as JSON-incapable.
func TestStartJSONBareRootReportsStartCommand(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"), 0644))

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--config", configFile, "--json")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command, "the bare invocation shares start's identity")
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "RUNTIME_UNAVAILABLE", envelope.Error.Code)
}

// Guards the hard-coded PlainSink rejectEndpointURL used here, which printed a
// human-readable line to stdout in addition to the envelope.
func TestStartJSONRejectsEndpointURLAsSingleEnvelope(t *testing.T) {
	t.Parallel()

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"start", "--json", "--endpoint-url", "http://localhost:4566")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)

	// decodeEnvelope is the assertion: it fails on anything but one JSON object.
	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	assert.Equal(t, "error", envelope.Status)
	require.NotNil(t, envelope.Error)
}

// The other plain-text leak: a fresh install emits a "Configured with default
// emulator" note that must not reach stdout under --json.
func TestStartJSONFirstRunEmitsSingleEnvelope(t *testing.T) {
	t.Parallel()

	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--json")
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "RUNTIME_UNAVAILABLE", envelope.Error.Code)
}

// The success payload: one entry per configured container, snapshotLoaded null.
func TestStartJSONSucceeds(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.APIEndpoint, mockServer.URL), "start", "--json")
	require.NoError(t, err, "lstk start --json failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Empty(t, stderr)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	assert.Equal(t, "ok", envelope.Status)
	assert.Nil(t, envelope.Error, "error must be null when status is ok")

	data := decodeStartData(t, envelope)
	require.Len(t, data.Emulators, 1, "one entry per configured container")
	emulator := data.Emulators[0]
	assert.Equal(t, "aws", emulator.Type)
	assert.Equal(t, containerName, emulator.Name)
	assert.Equal(t, "localhost.localstack.cloud:4566", emulator.Host)
	assert.NotEmpty(t, emulator.Version, "a started emulator reports its resolved version")
	assert.False(t, emulator.AlreadyRunning)
	assert.False(t, emulator.Persist)
	assert.Nil(t, data.SnapshotLoaded, "snapshotLoaded is null when nothing was auto-loaded")
}

// Starting against an emulator already up is still "ok", with alreadyRunning
// flipped. Mirrors TestStartCommandAttachesWhenLocalStackRespondingOnPort, so
// the version comes from the mock /_localstack/info and no real token is needed.
func TestStartJSONReportsAlreadyRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"3.4.0","edition":"pro"}`))
			return
		}
		http.NotFound(w, r)
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token"), "start", "--json")
	require.NoError(t, err, "lstk start --json failed against a running emulator: %s", stderr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	assert.Equal(t, "ok", envelope.Status)
	assert.Nil(t, envelope.Error)

	data := decodeStartData(t, envelope)
	require.Len(t, data.Emulators, 1)
	emulator := data.Emulators[0]
	assert.True(t, emulator.AlreadyRunning, "the emulator was already running")
	assert.Equal(t, "aws", emulator.Type)
	assert.Equal(t, "3.4.0", emulator.Version, "the running emulator's own reported version")
	assert.Equal(t, "localhost.localstack.cloud:4566", emulator.Host)
	assert.Nil(t, data.SnapshotLoaded)
}

// persist is otherwise only observable as a plain-text bullet.
func TestStartJSONReportsPersist(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.APIEndpoint, mockServer.URL), "start", "--persist", "--json")
	require.NoError(t, err, "lstk start --persist --json failed: %s", stderr)

	data := decodeStartData(t, decodeEnvelope(t, stdout))
	require.Len(t, data.Emulators, 1)
	assert.True(t, data.Emulators[0].Persist)
}

// Pins the AUTH_REQUIRED / exit-4 reservation. Docker must be healthy, since
// the runtime check precedes the auth check.
func TestStartJSONAuthRequiredExitsFour(t *testing.T) {
	requireDocker(t)

	cleanup()
	t.Cleanup(cleanup)

	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile,
		[]byte(fmt.Sprintf("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = %q\n", "4599")), 0644))

	// Isolated HOME empties the file keyring; Without drops the env var the
	// developer's shell or CI may have set. No token resolvable from any source.
	e := env.Environ(testEnvWithHome(home, "")).Without(env.AuthToken)
	stdout, stderr, err := runLstk(t, testContext(t), "", e,
		"--config", configFile, "start", "--json")
	requireExitCode(t, 4, err)
	assert.Empty(t, stderr)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "start", envelope.Command)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "AUTH_REQUIRED", envelope.Error.Code)
	assert.Equal(t, "AUTH", envelope.Error.Category)
	assert.False(t, envelope.Error.Retryable)
}
