package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietHelperEnv selects the behaviour of TestQuietHelperProcess, which the
// tests run as the extension (the test binary re-executing itself works on
// every OS, unlike a shell-script fixture).
const quietHelperEnv = "LSTK_TEST_QUIET_HELPER"

func TestQuietHelperProcess(t *testing.T) {
	mode := os.Getenv(quietHelperEnv)
	if mode == "" {
		return
	}
	fmt.Fprintln(os.Stderr, "helper stderr")
	switch mode {
	case "ok":
		data, _ := json.Marshal(map[string]string{
			"disableEvents": os.Getenv(disableEventsEnv),
			"context":       os.Getenv(EnvContext),
		})
		fmt.Printf(`{"schemaVersion":1,"status":"ok","data":%s}`, data)
	case "exit":
		os.Exit(3)
	case "garbage":
		fmt.Print("not json")
	case "error":
		fmt.Print(`{"schemaVersion":1,"status":"error","error":{"code":"IAC_FILE_NOT_FOUND","message":"nope"}}`)
	case "hang":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func quietHelper(t *testing.T, mode string) *Extension {
	t.Helper()
	t.Setenv(quietHelperEnv, mode)
	exe, err := os.Executable()
	require.NoError(t, err)
	return &Extension{Name: "helper", Path: exe, Argv0: "lstk-helper"}
}

var quietHelperArgs = []string{"-test.run=^TestQuietHelperProcess$"}

func TestInvokeQuietlyWithJSONReturnsDataAndEnforcesContext(t *testing.T) {
	// Parent telemetry on: the child must still get the opt-out.
	t.Setenv(disableEventsEnv, "0")
	ext := quietHelper(t, "ok")

	data, err := InvokeQuietlyWithJSON(t.Context(), ext, quietHelperArgs, QuietOptions{ConfigDir: "/cfg", AuthToken: "tok"})
	require.NoError(t, err)

	var got struct {
		DisableEvents string `json:"disableEvents"`
		Context       string `json:"context"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "1", got.DisableEvents)

	var ctx map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.Context), &ctx))
	assert.Equal(t, true, ctx["json"])
	assert.Equal(t, true, ctx["nonInteractive"])
	assert.Equal(t, "/cfg", ctx["configDir"])
	assert.Equal(t, "tok", ctx["authToken"])
	for _, key := range []string{"sessionId", "machineId", "endpointUrl"} {
		assert.NotContains(t, ctx, key)
	}
}

func TestInvokeQuietlyWithJSONFailures(t *testing.T) {
	for _, mode := range []string{"exit", "garbage", "error"} {
		t.Run(mode, func(t *testing.T) {
			_, err := InvokeQuietlyWithJSON(t.Context(), quietHelper(t, mode), quietHelperArgs, QuietOptions{})
			assert.Error(t, err)
		})
	}
}

func TestInvokeQuietlyWithJSONHonoursDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := InvokeQuietlyWithJSON(ctx, quietHelper(t, "hang"), quietHelperArgs, QuietOptions{})
	assert.Error(t, err)
	assert.Less(t, time.Since(started), 10*time.Second)
}

func TestInvokeQuietlyWithJSONWritesNothingToTheTerminal(t *testing.T) {
	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err)
	stderrR, stderrW, err := os.Pipe()
	require.NoError(t, err)
	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW

	_, runErr := InvokeQuietlyWithJSON(t.Context(), quietHelper(t, "ok"), quietHelperArgs, QuietOptions{})

	os.Stdout, os.Stderr = origStdout, origStderr
	require.NoError(t, stdoutW.Close())
	require.NoError(t, stderrW.Close())
	require.NoError(t, runErr)
	out, _ := io.ReadAll(stdoutR)
	errOut, _ := io.ReadAll(stderrR)
	assert.Empty(t, out)
	assert.Empty(t, errOut)
}
