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

func TestInspectRejectsNonAWSConfig(t *testing.T) {
	t.Parallel()
	configPath := writeInspectConfig(t, "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), inspectEnv(t),
		"--config", configPath, "--non-interactive", "inspect", "enable")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "App Inspector is only available for the AWS emulator")
}

func TestInspectJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		enabled  bool
		args     []string
		wantData string
	}{
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

func TestInspectJSONErrors(t *testing.T) {
	t.Parallel()
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
		{"non-AWS endpoint", []string{"--endpoint-url", snowflake.URL, "inspect", "enable"}, "EMULATOR_WRONG_TYPE"},
		{"non-AWS config", []string{"--config", snowflakeConfig, "inspect", "enable"}, "EMULATOR_NOT_CONFIGURED"},
		{"runtime unavailable", []string{"inspect", "enable"}, "RUNTIME_UNAVAILABLE"},
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
	assert.NotContains(t, stdout, "--persist")
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
	assert.NotContains(t, stdout, "--persist")
	assert.Equal(t, []string{"enabled"}, puts())
	assertCommandTelemetry(t, events, "inspect enable", 0)
}

func TestInspectEnableJSONLocalEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, puts := appInspectorServer(t, false)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.DisableEvents, "1").
			With(env.LocalStackHost, lsHost(srv)),
		"--json", "inspect", "enable")

	require.NoError(t, err, stderr)
	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	assert.Equal(t, "inspect enable", envelope.Command)
	assert.JSONEq(t, `{"enabled": true, "changed": true}`, string(envelope.Data))
	assert.Equal(t, []string{"enabled"}, puts())
}

func TestInspectEnableJSONNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.DisableEvents, "1"),
		"--json", "inspect", "enable")

	requireExitCode(t, 1, err)
	envelope := decodeEnvelope(t, stdout)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "EMULATOR_NOT_RUNNING", envelope.Error.Code)
}
