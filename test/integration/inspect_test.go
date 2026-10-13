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
		{"enable when already on", true, "enable", "App Inspector is enabled", []string{"enabled"}},
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
			assert.Contains(t, stdout, "on every start ("+configPath+")")
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

func TestInspectJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		enabled  bool
		args     []string
		wantData string
	}{
		{"status", true, []string{"inspect", "status"}, `{"enabled": true, "changed": false}`},
		{"enable", false, []string{"inspect", "enable"}, `{"enabled": true, "changed": true}`},
		{"disable", true, []string{"inspect", "disable"}, `{"enabled": false, "changed": true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := appInspectorServer(t, tc.enabled)

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
				append([]string{"--endpoint-url", srv.URL, "--json"}, tc.args...)...)

			require.NoError(t, err, stderr)
			envelope := decodeEnvelope(t, stdout)
			assert.Equal(t, "ok", envelope.Status)
			assert.Equal(t, strings.Join(tc.args, " "), envelope.Command)
			assert.JSONEq(t, tc.wantData, string(envelope.Data))
		})
	}
}

func TestInspectPersistJSON(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, inspectBaseConfig)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--json", "inspect", "enable", "--persist")

	require.NoError(t, err, stderr)
	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	assert.JSONEq(t, `{"saved": {"enabled": true, "changed": true}}`, string(envelope.Data))
}

func TestInspectJSONErrors(t *testing.T) {
	t.Parallel()
	aws, _ := appInspectorServer(t, false)
	snowflake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0", "services": map[string]string{"snowflake": "running"}})
	}))
	t.Cleanup(snowflake.Close)
	snowflakeConfig := writeInspectConfig(t, "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n")

	for _, tc := range []struct {
		name     string
		args     []string
		wantCode string
	}{
		{"persist with endpoint", []string{"--endpoint-url", aws.URL, "inspect", "enable", "--persist"}, "VALIDATION_ERROR"},
		{"non-AWS endpoint", []string{"--endpoint-url", snowflake.URL, "inspect", "status"}, "EMULATOR_WRONG_TYPE"},
		{"non-AWS config", []string{"--config", snowflakeConfig, "inspect", "status"}, "EMULATOR_NOT_CONFIGURED"},
		{"persist without config file", []string{"inspect", "enable", "--persist"}, "CONFIG_NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t), append([]string{"--json"}, tc.args...)...)

			requireExitCode(t, 1, err)
			envelope := decodeEnvelope(t, stdout)
			require.NotNil(t, envelope.Error)
			assert.Equal(t, tc.wantCode, envelope.Error.Code)
		})
	}
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

// The emulator turns App Inspector on shortly after it reports ready, so the check waits.
func TestInspectPersistAppliesOnStart(t *testing.T) {
	requireDocker(t)
	token := env.Require(t, env.AuthToken)
	cleanup()
	home := t.TempDir()
	scheduleVolumeCleanup(t, home)
	// Runs first (LIFO), so the emulator stops before its volume is removed.
	t.Cleanup(cleanup)

	ctx := testContext(t)
	configPath := writeInspectConfig(t, inspectBaseConfig)
	e := env.Environ(testEnvWithHome(home, "")).With(env.AuthToken, token)

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "inspect", "enable", "--persist")
	require.NoError(t, err, stderr)

	_, stderr, err = runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "start")
	require.NoError(t, err, stderr)

	require.Eventually(t, func() bool {
		stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "inspect", "status")
		return err == nil && strings.Contains(stdout, "App Inspector is enabled")
	}, 15*time.Second, 500*time.Millisecond)
}
