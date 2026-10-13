package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appInspectorServer mimics the AWS emulator's App Inspector switch and
// records the status sent by each PUT.
func appInspectorServer(t *testing.T, enabled bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var puts []string
	health := awsHealthHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_localstack/appinspector/status" {
			health.ServeHTTP(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		changed := false
		if r.Method == http.MethodPut {
			var req struct {
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			puts = append(puts, req.Status)
			changed = enabled != (req.Status == "enabled")
			enabled = req.Status == "enabled"
		}
		status := "DISABLED"
		if enabled {
			status = "ENABLED"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "changed": changed})
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), puts...)
	}
}

// inspectEnv points Docker at a closed port, so tests using it prove the
// command never needed Docker.
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

func TestInspectEndpointURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		enabled  bool
		cmd      string
		want     string
		wantPuts []string
	}{
		{"enable", false, "enable", "App Inspector enabled", []string{"enabled"}},
		{"enable when already on", true, "enable", "App Inspector is already enabled", []string{"enabled"}},
		{"disable", true, "disable", "App Inspector disabled", []string{"disabled"}},
		{"status on", true, "status", "App Inspector is enabled", nil},
		{"status off", false, "status", "App Inspector is disabled", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, puts := appInspectorServer(t, tc.enabled)

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
				"--endpoint-url", srv.URL, "--non-interactive", "inspect", tc.cmd)

			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, tc.want)
			assert.Equal(t, tc.wantPuts, puts())
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
	srv, puts := appInspectorServer(t, false)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--endpoint-url", srv.URL, "--non-interactive", "inspect", "enable", "--persist")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "--persist")
	assert.Empty(t, puts())
}

// With the emulator stopped, --persist only saves the setting. A second run
// finds nothing to change.
func TestInspectPersist(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cmd  string
		in   string
		want string
	}{
		{
			name: "enable adds a profile and keeps comments",
			cmd:  "enable",
			in:   inspectBaseConfig,
			want: `# my settings
[[containers]]
env = ["app-inspector"]
type = "aws"     # emulator type
tag  = "latest"
port = "4566"
# env = []       # Named environment profiles

[cli]
check_for_update_on_startup = false

[env.app-inspector]
APP_INSPECTOR = "1"
`,
		},
		{
			name: "enable keeps existing profiles",
			cmd:  "enable",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"debug\"]\n\n[env.debug]\nDEBUG = \"1\"\n",
			want: "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"debug\", \"app-inspector\"]\n\n[env.debug]\nDEBUG = \"1\"\n\n[env.app-inspector]\nAPP_INSPECTOR = \"1\"\n",
		},
		{
			name: "disable flips the saved value",
			cmd:  "disable",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"app-inspector\"]\n\n[env.app-inspector]\nAPP_INSPECTOR = \"1\"\n",
			want: "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"app-inspector\"]\n\n[env.app-inspector]\nAPP_INSPECTOR = \"0\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configPath := writeInspectConfig(t, tc.in)
			e := inspectEnv(t)
			args := []string{"--config", configPath, "--non-interactive", "inspect", tc.cmd, "--persist"}

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, args...)
			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, "next start")
			assert.Equal(t, tc.want, readFile(t, configPath))

			stdout, stderr, err = runLstk(t, testContext(t), t.TempDir(), e, args...)
			require.NoError(t, err, stderr)
			assert.Contains(t, stdout, "already")
			assert.Equal(t, tc.want, readFile(t, configPath))
		})
	}
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
	srv, puts := appInspectorServer(t, false)
	analyticsSrv, events := mockAnalyticsServer(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "inspect", "enable")

	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "App Inspector enabled")
	assert.Contains(t, stdout, "lstk inspect enable --persist")
	assert.Equal(t, []string{"enabled"}, puts())
	assertCommandTelemetry(t, events, "inspect enable", 0)
}

// The saved setting must reach a real emulator on its next start. The emulator
// turns App Inspector on shortly after it reports ready, so the check waits.
func TestInspectPersistAppliesOnStart(t *testing.T) {
	requireDocker(t)
	token := env.Require(t, env.AuthToken)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	configPath := writeInspectConfig(t, inspectBaseConfig)
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, token)

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)

	_, stderr, err = runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "start")
	require.NoError(t, err, stderr)

	require.Eventually(t, func() bool {
		stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "inspect", "status")
		return err == nil && strings.Contains(stdout, "App Inspector is enabled")
	}, 15*time.Second, 500*time.Millisecond)
}
