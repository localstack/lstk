package integration_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxyTelemetryCase runs one proxy against its fake tool without Docker.
type proxyTelemetryCase struct {
	// toolCases answer lstk's own pre-run calls (version checks) without
	// counting as the user's invocation.
	toolCases []fakeToolCase
	// args returns the full lstk argv, including any emulator it needs.
	args func(t *testing.T) []string
}

var proxyTelemetryCases = map[string]proxyTelemetryCase{
	"aws": {
		args: func(t *testing.T) []string {
			srv := awsHealthServer(t)
			t.Cleanup(srv.Close)
			return []string{"--endpoint-url", srv.URL, "aws", "s3", "ls"}
		},
	},
	"az": {
		args: func(t *testing.T) []string {
			return []string{"--endpoint-url", azureHealthServer(t).URL, "--config", azureConfigWithSetupMarker(t),
				"--non-interactive", "az", "group", "list"}
		},
	},
	"cdk": {
		toolCases: []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"2.177.0"}}},
		args:      func(*testing.T) []string { return []string{"cdk", "synth"} },
	},
	"sam": {
		toolCases: []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"SAM CLI, version 1.95.0"}}},
		args:      func(*testing.T) []string { return []string{"sam", "build"} },
	},
	"terraform": {
		args: func(*testing.T) []string { return []string{"terraform", "validate"} },
	},
}

// Every proxy must mark its tool's exit with proc.MarkUserToolExit and return
// nil only after running the tool, because a proxied nil records as
// proxy_exit_code 0. The proxy set is read from the Tools section of `lstk
// --help`, so a new proxy fails here until it has a case.
func TestEveryProxyRecordsItsToolExitInTelemetry(t *testing.T) {
	t.Parallel()

	var want []string
	for name := range proxyTelemetryCases {
		want = append(want, name)
	}
	sort.Strings(want)
	require.Equal(t, want, helpToolsSection(t), "every Tools command needs a proxyTelemetryCases entry")

	for _, name := range want {
		tc := proxyTelemetryCases[name]
		t.Run(name+" failure", func(t *testing.T) {
			t.Parallel()
			analyticsSrv, events := mockAnalyticsServer(t)
			fakeBinDir := writeFakeTool(t, name, fakeToolConfig{Cases: tc.toolCases, ExitCode: 3})

			_, _, err := runLstk(t, testContext(t), t.TempDir(), proxyEnviron(t, analyticsSrv.URL, fakeBinDir), tc.args(t)...)
			requireExitCode(t, 3, err)

			params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
			assert.Equal(t, true, params["proxied"])
			require.Contains(t, result, "proxy_exit_code", "the tool's exit must be marked with proc.MarkUserToolExit")
			assert.InDelta(t, 3, result["proxy_exit_code"], 0)
		})
		t.Run(name+" success", func(t *testing.T) {
			t.Parallel()
			analyticsSrv, events := mockAnalyticsServer(t)
			ranFile := filepath.Join(t.TempDir(), "ran")
			fakeBinDir := writeFakeTool(t, name, fakeToolConfig{Cases: tc.toolCases, RecordFile: ranFile, RecordContent: "{args}"})

			_, stderr, err := runLstk(t, testContext(t), t.TempDir(), proxyEnviron(t, analyticsSrv.URL, fakeBinDir), tc.args(t)...)
			require.NoError(t, err, "stderr: %s", stderr)

			params, result := commandEventParts(t, receiveEventByName(t, events, "lstk_command"))
			assert.Equal(t, true, params["proxied"])
			assert.InDelta(t, 0, result["proxy_exit_code"], 0)
			assert.FileExists(t, ranFile, "proxy_exit_code 0 was recorded, so the tool must have run")
		})
	}
}

// helpToolsSection returns the command names listed under "Tools:" in `lstk
// --help`, which NewRootCmd builds from the same slice that annotates proxies.
func helpToolsSection(t *testing.T) []string {
	t.Helper()
	stdout, _, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "--help")
	require.NoError(t, err)

	_, section, found := strings.Cut(stdout, "\nTools:\n")
	require.True(t, found, "no Tools section in help:\n%s", stdout)
	section, _, _ = strings.Cut(section, "\n\n")

	var names []string
	for _, line := range strings.Split(section, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	sort.Strings(names)
	return names
}
