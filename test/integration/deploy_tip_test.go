package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const deployTipText = "> Tip: Deploy your Terraform project to LocalStack: lstk deploy"

// deployTipAttempts bounds the start loop. The deploy tip is one of three AWS
// rotating tips, so 20 runs miss it with probability (2/3)^20, about 0.03%.
const deployTipAttempts = 20

const detectEnvelope = `{"schemaVersion":1,"command":"deploy detect","status":"ok","data":{"tools":["terraform"],"matches":[]}}`

// fakeDeployOutputMarker is printed by every fake lstk-deploy on stderr (and
// as the garbage stdout variant), so a leak of the detector's output into
// lstk's own output is detectable.
const fakeDeployOutputMarker = "FAKE-DEPLOY-OUTPUT"

// deployTipEnv is an isolated, non-first-run AWS setup whose only resolvable
// deploy extension is the fake in fakeDir. It returns the env, a copy of lstk
// in an otherwise empty dir (so no real bundle beside bin/lstk is picked up),
// and an IaC workspace to run from.
func deployTipEnv(t *testing.T, fakeDir string) (env.Environ, string, string) {
	t.Helper()
	home := t.TempDir()
	e := env.Environ(envWithPath(home, fakeDir)).
		Without(env.DisableEvents).
		With(env.AuthToken, "fake-token")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath,
		[]byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	lstkBin := installLstkBundle(t, t.TempDir())

	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "main.tf"),
		[]byte("resource \"aws_s3_bucket\" \"b\" {}\n"), 0644))
	return e, lstkBin, workspace
}

func fakeDeploy(recordFile string, cfg fakeToolConfig) fakeToolConfig {
	cfg.RecordFile = recordFile
	cfg.RecordContent = "{args}\n{env:LOCALSTACK_DISABLE_EVENTS}\n{env:LSTK_EXT_CONTEXT}"
	cfg.Stderr = append(cfg.Stderr, fakeDeployOutputMarker)
	return cfg
}

func fakeDeployOK(recordFile string) fakeToolConfig {
	return fakeToolConfig{
		Stdout:        []string{detectEnvelope},
		Stderr:        []string{fakeDeployOutputMarker},
		RecordFile:    recordFile,
		RecordContent: "{args}\n{env:LOCALSTACK_DISABLE_EVENTS}\n{env:LSTK_EXT_CONTEXT}",
	}
}

func assertOneTip(t *testing.T, stdout, stderr string) string {
	t.Helper()
	tips := distinctTips(stdout + "\n" + stderr)
	require.Len(t, tips, 1, "every start shows exactly one tip:\n%s", stdout)
	return tips[0]
}

func TestDeployTipShownForIaCDirectory(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	startTestContainer(t, testContext(t))

	recordFile := filepath.Join(t.TempDir(), "detect.record")
	fakeDir := writeFakeTool(t, "lstk-deploy", fakeDeployOK(recordFile))
	e, lstkBin, workspace := deployTipEnv(t, fakeDir)

	shown := false
	for i := 0; i < deployTipAttempts && !shown; i++ {
		_ = os.Remove(recordFile)
		stdout, stderr, err := runBinary(t, workspace, e, lstkBin, "start")
		require.NoError(t, err, "start against the running emulator should succeed: %s", stderr)
		assert.NotContains(t, stdout+stderr, fakeDeployOutputMarker, "detector output must never reach the user")

		tip := assertOneTip(t, stdout, stderr)
		if tip != deployTipText {
			assert.NoFileExists(t, recordFile, "detect must not run unless the deploy tip was chosen (tip shown: %q)", tip)
			continue
		}
		shown = true

		record, err := os.ReadFile(recordFile)
		require.NoError(t, err, "the deploy tip must come from a detect run")
		lines := strings.SplitN(string(record), "\n", 3)
		require.Len(t, lines, 3)
		assert.Equal(t, "detect", lines[0])
		assert.Equal(t, "1", lines[1], "the detect run must have its own telemetry disabled")

		var ctx map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[2]), &ctx), "LSTK_EXT_CONTEXT: %s", lines[2])
		assert.Equal(t, true, ctx["json"])
		assert.Equal(t, true, ctx["nonInteractive"])
		assert.Equal(t, "fake-token", ctx["authToken"])
		assert.NotContains(t, ctx, "sessionId")
		assert.NotContains(t, ctx, "machineId")
	}
	assert.True(t, shown, "the deploy tip never appeared in %d starts", deployTipAttempts)
}

func TestDeployTipFallsBackWhenDetectorDeclines(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	startTestContainer(t, testContext(t))

	for _, tc := range []struct {
		name string
		cfg  fakeToolConfig
		// hangs means the fake never gets to record; that it was chosen shows
		// instead as a start that waited for the detector deadline.
		hangs bool
	}{
		{name: "no tools", cfg: fakeToolConfig{Stdout: []string{
			`{"schemaVersion":1,"command":"deploy detect","status":"ok","data":{"tools":[],"matches":[]}}`}}},
		{name: "non-zero exit", cfg: fakeToolConfig{ExitCode: 3}},
		{name: "garbage output", cfg: fakeToolConfig{Stdout: []string{fakeDeployOutputMarker}}},
		{name: "hangs past the deadline", cfg: fakeToolConfig{SleepSeconds: 30}, hangs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recordFile := filepath.Join(t.TempDir(), "detect.record")
			fakeDir := writeFakeTool(t, "lstk-deploy", fakeDeploy(recordFile, tc.cfg))
			e, lstkBin, workspace := deployTipEnv(t, fakeDir)

			chosen := false
			for i := 0; i < deployTipAttempts && !chosen; i++ {
				_ = os.Remove(recordFile)
				started := time.Now()
				stdout, stderr, err := runBinary(t, workspace, e, lstkBin, "start")
				elapsed := time.Since(started)
				require.NoError(t, err, "a declining detector must not fail the start: %s", stderr)
				assert.NotContains(t, stdout+stderr, fakeDeployOutputMarker, "detector output must never reach the user")

				tip := assertOneTip(t, stdout, stderr)
				assert.NotContains(t, tip, "lstk deploy", "a declining detector must not produce the deploy tip")

				if tc.hangs {
					assert.Less(t, elapsed, 15*time.Second, "the start must not wait for a hung detector")
					chosen = elapsed >= 2*time.Second
				} else {
					_, statErr := os.Stat(recordFile)
					chosen = statErr == nil
				}
			}
			assert.True(t, chosen, "the deploy detector was never chosen in %d starts", deployTipAttempts)
		})
	}
}

func TestDeployTipNeverRunsDetectorOnFirstRun(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	startTestContainer(t, testContext(t))

	recordFile := filepath.Join(t.TempDir(), "detect.record")
	fakeDir := writeFakeTool(t, "lstk-deploy", fakeDeployOK(recordFile))
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0755))
	e := env.Environ(envWithPath(home, fakeDir)).With(env.AuthToken, "fake-token")
	lstkBin := installLstkBundle(t, t.TempDir())
	workspace := t.TempDir()

	cmd := exec.CommandContext(testContext(t), lstkBin, "start")
	cmd.Dir = workspace
	cmd.Env = e
	p := startCmdInPTY(t, testContext(t), cmd)
	p.waitForOutput("Which emulator would you like to use?", "a fresh home should be a first run")
	p.write("\r")

	out, err := p.wait()
	require.NoError(t, err, "first run should succeed against the running emulator")
	assert.Equal(t, []string{completionTipText}, distinctTips(out), "first run shows only the completion tip")
	assert.NoFileExists(t, recordFile, "no detector runs when the completion tip wins")
}
