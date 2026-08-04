package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// TestCommandTelemetryPerCommand preserves the per-command telemetry coverage
// that used to live alongside behavioural assertions in logs_test.go,
// reset_test.go, volume_test.go, stop_test.go, restart_test.go, status_test.go,
// config_test.go, logout_test.go, and aws_cmd_test.go, before those behavioural
// cases were ported to test/e2e (which deliberately does not assert against a
// mock analytics server — see each *.pty.test.ts/*.test.ts file's "Dropped" note).
//
// Every case here arranges its state through the CLI or through an isolated
// HOME. None of them reach into credential storage: where a credential lives is
// an implementation detail, and the tests that did reach in were exactly the
// ones that behaved differently per platform.
//
// telemetry_test.go already covers the telemetry *mechanism* (disabled,
// unreachable endpoint, detached flusher, OTel); this test only proves each
// command still reports its own command name and exit code via lstk_command.
//
// "stop" (exit 0) and "status" (exit 0) are not repeated here: the former is
// already covered by TestStopCommandSendsTelemetryEvents in telemetry_test.go,
// and the latter by TestStatusCommandShowsResourcesWhenRunning, which stays in
// status_test.go (it needs an AWS SDK client, not just telemetry).
//
// "login" is likewise absent: its telemetry assertion lives in
// TestDeviceFlowSuccess, entangled with the PTY device flow (mock platform plus
// a fake browser opener), which is not worth reproducing here.
func TestCommandTelemetryPerCommand(t *testing.T) {
	type telemetryCase struct {
		name             string
		requireDocker    bool
		requireAuthToken bool
		// run performs whatever setup the original test needed (starting a
		// placeholder container, standing in a mock HTTP endpoint, etc.) and
		// invokes lstk, routing its telemetry at analyticsURL.
		run          func(t *testing.T, ctx context.Context, analyticsURL string) (stdout, stderr string, err error)
		wantCommand  string
		wantExitCode int
	}

	cases := []telemetryCase{
		{
			name:          "logs succeeds against a running emulator",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				startTestContainer(t, ctx)

				configFile := filepath.Join(t.TempDir(), "config.toml")
				writeConfigFile(t, configFile)
				return runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsURL), "--config", configFile, "logs")
			},
			wantCommand:  "logs",
			wantExitCode: 0,
		},
		{
			name:          "logs fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)

				configFile := filepath.Join(t.TempDir(), "config.toml")
				writeConfigFile(t, configFile)
				return runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsURL), "--config", configFile, "logs", "--follow")
			},
			wantCommand:  "logs",
			wantExitCode: 1,
		},
		{
			name:          "reset succeeds with --force",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				startTestContainer(t, ctx)

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/_localstack/state/reset" && r.Method == http.MethodPost {
						w.WriteHeader(http.StatusOK)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))
				t.Cleanup(srv.Close)

				e := env.Environ(testEnvWithHome(t.TempDir(), "")).
					With(env.LocalStackHost, lsHost(srv)).
					With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "--non-interactive", "reset", "--force")
			},
			wantCommand:  "reset",
			wantExitCode: 0,
		},
		{
			name:          "reset fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)

				e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "--non-interactive", "reset", "--force")
			},
			wantCommand:  "reset",
			wantExitCode: 1,
		},
		{
			name: "volume path",
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				tmpHome := t.TempDir()
				xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
				configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
				writeConfigFile(t, configFile)

				e := env.Environ(testEnvWithHome(tmpHome, xdgOverride)).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "volume", "path")
			},
			wantCommand:  "volume path",
			wantExitCode: 0,
		},
		{
			name: "volume clear",
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				tmpHome := t.TempDir()
				xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
				configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
				writeConfigFile(t, configFile)

				e := env.Environ(testEnvWithHome(tmpHome, xdgOverride)).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "--non-interactive", "volume", "clear", "--force")
			},
			wantCommand:  "volume clear",
			wantExitCode: 0,
		},
		{
			name:          "stop fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				return runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsURL), "stop")
			},
			wantCommand:  "stop",
			wantExitCode: 1,
		},
		{
			name:          "restart fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				return runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsURL), "restart")
			},
			wantCommand:  "restart",
			wantExitCode: 1,
		},
		{
			name:             "restart succeeds against a running emulator",
			requireDocker:    true,
			requireAuthToken: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)

				mockServer := createMockLicenseServer(true)
				t.Cleanup(mockServer.Close)

				_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
				require.NoError(t, err, "lstk start failed: %s", stderr)

				e := env.With(env.APIEndpoint, mockServer.URL).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, "", e, "restart")
			},
			wantCommand:  "restart",
			wantExitCode: 0,
		},
		{
			name:          "status fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				return runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsURL), "status")
			},
			wantCommand:  "status",
			wantExitCode: 1,
		},
		{
			name: "logout with nothing stored",
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				// A fresh HOME on the file keyring holds no credential by
				// construction, so arranging this needs no reach into storage.
				e := env.Environ(testEnvWithHome(t.TempDir(), "")).
					Without(env.AuthToken).
					With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "logout")
			},
			wantCommand:  "logout",
			wantExitCode: 0,
		},
		{
			name:          "aws succeeds against a running emulator",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)
				startTestContainer(t, ctx)

				// The fake `aws` keeps the real AWS CLI out of the picture; a fresh
				// HOME keeps a developer's own localstack profile out of it too.
				e := env.With(env.Path, writeFakeAWS(t)).
					With(env.Home, t.TempDir()).
					With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
			},
			wantCommand:  "aws",
			wantExitCode: 0,
		},
		{
			name:          "aws fails when the emulator is not running",
			requireDocker: true,
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				cleanup()
				t.Cleanup(cleanup)

				e := env.With(env.Path, writeFakeAWS(t)).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
			},
			wantCommand:  "aws",
			wantExitCode: 1,
		},
		{
			name: "config path",
			run: func(t *testing.T, ctx context.Context, analyticsURL string) (string, string, error) {
				tmpHome := t.TempDir()
				workDir := t.TempDir()
				xdgConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
				writeConfigFile(t, xdgConfigFile)

				e := env.Environ(testEnvWithHome(tmpHome, filepath.Join(tmpHome, "xdg-config-home"))).With(env.AnalyticsEndpoint, analyticsURL)
				return runLstk(t, ctx, workDir, e, "config", "path")
			},
			wantCommand:  "config path",
			wantExitCode: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.requireDocker {
				requireDocker(t)
			}
			if c.requireAuthToken {
				// Skip rather than env.Require's hard fail: the test this case came
				// from was a standalone one, where failing without a token reddened
				// only itself. Here it would redden the whole table for anyone
				// without a token, so use the package's skipping helper instead.
				requireAuthToken(t)
			}

			ctx := testContext(t)
			analyticsSrv, events := mockAnalyticsServer(t)

			_, stderr, err := c.run(t, ctx, analyticsSrv.URL)
			requireExitCode(t, c.wantExitCode, err)
			if c.wantExitCode != 0 {
				t.Logf("stderr: %s", stderr)
			}

			assertCommandTelemetry(t, events, c.wantCommand, c.wantExitCode)
		})
	}
}
