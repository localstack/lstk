package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appInspectorServer mimics an AWS emulator's /_localstack/appinspector/status
// endpoint (GET reports the state, PUT {"status": "enabled"|"disabled"} sets it)
// and records the status sent by each PUT.
type appInspectorServer struct {
	*httptest.Server
	mu      sync.Mutex
	enabled bool
	puts    []string
}

func newAppInspectorServer(t *testing.T, enabled bool) *appInspectorServer {
	t.Helper()
	s := &appInspectorServer{enabled: enabled}
	health := awsHealthHandler()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/appinspector/status" {
			health.ServeHTTP(w, r)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": statusWord(s.enabled), "note": "", "epoch": nil})
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.puts = append(s.puts, req.Status)
			want := req.Status == "enabled"
			changed := want != s.enabled
			s.enabled = want
			_ = json.NewEncoder(w).Encode(map[string]any{"status": statusWord(s.enabled), "changed": changed})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func statusWord(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "DISABLED"
}

func (s *appInspectorServer) recordedPuts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.puts...)
}

// inspectEnv is an isolated environment whose Docker endpoint is unreachable, so
// any test using it proves the command never needed Docker.
func inspectEnv(t *testing.T) env.Environ {
	t.Helper()
	return env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.DisableEvents, "1").
		With(dockerHostKey, "tcp://localhost:1")
}

func writeInspectConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

const inspectBaseConfig = `# my settings
[[containers]]
type = "aws"     # emulator type
tag  = "latest"
port = "4566"
# env = []       # Named environment profiles

[cli]
check_for_update_on_startup = false
`

func TestInspectEnableEndpointURL(t *testing.T) {
	t.Parallel()
	srv := newAppInspectorServer(t, false)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "enable")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector enabled")
	assert.Equal(t, []string{"enabled"}, srv.recordedPuts())
}

func TestInspectEnableWhenAlreadyEnabled(t *testing.T) {
	t.Parallel()
	srv := newAppInspectorServer(t, true)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "enable")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector is already enabled")
}

func TestInspectDisableEndpointURL(t *testing.T) {
	t.Parallel()
	srv := newAppInspectorServer(t, true)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "disable")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector disabled")
	assert.Equal(t, []string{"disabled"}, srv.recordedPuts())
}

func TestInspectStatusEndpointURL(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		want := "App Inspector is disabled"
		if enabled {
			want = "App Inspector is enabled"
		}
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			srv := newAppInspectorServer(t, enabled)

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
				"--endpoint-url", srv.URL, "--non-interactive", "inspect", "status")

			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, want)
			assert.Empty(t, srv.recordedPuts(), "status must not change the state")
		})
	}
}

// An emulator older than App Inspector has no status route and answers 404.
func TestInspectFailsWhenEmulatorLacksAppInspector(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	t.Cleanup(srv.Close)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "enable")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "App Inspector is not available in this emulator")
}

func TestInspectPersistRejectsEndpointURL(t *testing.T) {
	t.Parallel()
	srv := newAppInspectorServer(t, false)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "enable", "--persist")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "--persist")
	assert.Empty(t, srv.recordedPuts(), "a rejected invocation must not change the emulator")
}

// With the emulator stopped, --persist saves the setting so the next start
// picks it up, without needing Docker at all.
func TestInspectEnablePersistWhileStoppedWritesConfig(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, inspectBaseConfig)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "next start")
	assert.Equal(t, `# my settings
[[containers]]
type = "aws"     # emulator type
env = ["app-inspector"]
tag  = "latest"
port = "4566"
# env = []       # Named environment profiles

[cli]
check_for_update_on_startup = false

[env.app-inspector]
APP_INSPECTOR = "1"
`, readFile(t, configPath))
}

func TestInspectEnablePersistKeepsExistingProfiles(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, `[[containers]]
type = "aws"
port = "4566"
env = ["debug"]

[env.debug]
DEBUG = "1"
`)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	require.NoError(t, err, stderr)
	assert.Equal(t, `[[containers]]
type = "aws"
port = "4566"
env = ["debug", "app-inspector"]

[env.debug]
DEBUG = "1"

[env.app-inspector]
APP_INSPECTOR = "1"
`, readFile(t, configPath))
}

func TestInspectEnablePersistIsIdempotent(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, inspectBaseConfig)
	e := inspectEnv(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)
	afterFirst := readFile(t, configPath)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "already")
	assert.Equal(t, afterFirst, readFile(t, configPath))
}

// A profile the user wrote themselves already enables App Inspector, so there
// is nothing to save.
func TestInspectEnablePersistRespectsUserProfile(t *testing.T) {
	t.Parallel()
	original := `[[containers]]
type = "aws"
port = "4566"
env = ["mine"]

[env.mine]
APP_INSPECTOR = "true"
`
	configPath := writeInspectConfig(t, original)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "already")
	assert.Equal(t, original, readFile(t, configPath))
}

func TestInspectDisablePersistAfterEnable(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, inspectBaseConfig)
	e := inspectEnv(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)

	_, stderr, err = runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configPath, "--non-interactive", "inspect", "disable", "--persist")
	require.NoError(t, err, stderr)

	got := readFile(t, configPath)
	assert.Contains(t, got, "[env.app-inspector]\nAPP_INSPECTOR = \"0\"\n")
	assert.Equal(t, 1, strings.Count(got, `"app-inspector"`), "profile must be referenced once")
}

// A profile that turns App Inspector on is overridden by lstk's own profile,
// which is applied last.
func TestInspectDisablePersistOverridesUserProfile(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, `[[containers]]
type = "aws"
port = "4566"
env = [
  "mine",
]

[env.mine]
APP_INSPECTOR = "1"
`)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "disable", "--persist")

	require.NoError(t, err, stderr)
	assert.Equal(t, `[[containers]]
type = "aws"
port = "4566"
env = [
  "mine", "app-inspector",
]

[env.mine]
APP_INSPECTOR = "1"

[env.app-inspector]
APP_INSPECTOR = "0"
`, readFile(t, configPath))
}

// lstk's profile is listed before one that also sets APP_INSPECTOR, so saving
// would have no effect; the file must be left as it was.
func TestInspectPersistFailsWhenALaterProfileOverrides(t *testing.T) {
	t.Parallel()
	original := `[[containers]]
type = "aws"
port = "4566"
env = ["app-inspector", "mine"]

[env.app-inspector]
APP_INSPECTOR = "0"

[env.mine]
APP_INSPECTOR = "0"
`
	configPath := writeInspectConfig(t, original)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Could not save the App Inspector setting")
	assert.Contains(t, stdout, "[env.mine]")
	assert.Equal(t, original, readFile(t, configPath))
}

func TestInspectDisablePersistWhenNeverEnabledLeavesConfigAlone(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, inspectBaseConfig)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "disable", "--persist")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "already")
	assert.Equal(t, inspectBaseConfig, readFile(t, configPath))
}

func TestInspectPersistWithoutConfigFile(t *testing.T) {
	t.Parallel()
	e := inspectEnv(t)
	configPath := resolvedConfigPath(t, e)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--non-interactive", "inspect", "enable", "--persist")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "lstk start")
	assert.NoFileExists(t, configPath)
}

func TestInspectRejectsNonAWSConfig(t *testing.T) {
	t.Parallel()
	original := "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n"
	configPath := writeInspectConfig(t, original)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "App Inspector is only available for the AWS emulator")
	assert.Equal(t, original, readFile(t, configPath))
}

func TestInspectEnableNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "inspect", "enable")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
	assert.Contains(t, stdout, "--persist")
}

func TestInspectEnableLocalEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := newAppInspectorServer(t, false)
	analyticsSrv, events := mockAnalyticsServer(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv.Server)).
			With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "inspect", "enable")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector enabled")
	assert.Contains(t, stdout, "lstk inspect enable --persist", "should explain how to keep it on after a restart")
	assert.Equal(t, []string{"enabled"}, srv.recordedPuts())
	assertCommandTelemetry(t, events, "inspect enable", 0)
}

func TestInspectEnablePersistLocalEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := newAppInspectorServer(t, false)
	configPath := writeInspectConfig(t, inspectBaseConfig)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv.Server)),
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector enabled")
	assert.NotContains(t, stdout, "next start")
	assert.Equal(t, []string{"enabled"}, srv.recordedPuts())
	assert.Contains(t, readFile(t, configPath), "[env.app-inspector]\nAPP_INSPECTOR = \"1\"\n")
}

// The persisted setting must reach a real emulator on its next start.
func TestInspectPersistAppliesOnStart(t *testing.T) {
	requireDocker(t)
	token := env.Require(t, env.AuthToken)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	configPath := writeInspectConfig(t, inspectBaseConfig)
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, token)

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e,
		"--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)

	_, stderr, err = runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "start")
	require.NoError(t, err, stderr)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "inspect", "status")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector is enabled")
}
