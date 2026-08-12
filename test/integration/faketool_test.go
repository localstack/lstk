package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeToolConfig and fakeToolCase mirror the config structs in
// test-samples/faketool/main.go; keep them in sync. They drive the compiled
// cross-platform stand-in for external CLIs (aws, az, terraform, cdk, sam,
// aws_completer, rundll32) that replaced the unix-only shell-script fakes, so
// the same tests run on Windows.
type fakeToolCase struct {
	Args     []string `json:"args"`
	Shift    int      `json:"shift,omitempty"`
	Stdout   []string `json:"stdout,omitempty"`
	Stderr   []string `json:"stderr,omitempty"`
	ExitCode int      `json:"exitCode,omitempty"`
}

type fakeToolConfig struct {
	Cases         []fakeToolCase `json:"cases,omitempty"`
	SleepSeconds  int            `json:"sleepSeconds,omitempty"`
	Shift         int            `json:"shift,omitempty"`
	Stdout        []string       `json:"stdout,omitempty"`
	Stderr        []string       `json:"stderr,omitempty"`
	RecordFile    string         `json:"recordFile,omitempty"`
	RecordContent string         `json:"recordContent,omitempty"`
	DumpFile      string         `json:"dumpFile,omitempty"`
	DumpPrefix    string         `json:"dumpPrefix,omitempty"`
	ExitCode      int            `json:"exitCode,omitempty"`
}

var (
	fakeToolOnce sync.Once
	fakeToolPath string
	fakeToolErr  error
)

// fakeToolBinary builds test-samples/faketool once and returns the path to the
// compiled binary, following the referenceExtensionBinary pattern.
func fakeToolBinary(t *testing.T) string {
	t.Helper()
	fakeToolOnce.Do(func() {
		moduleRoot, err := filepath.Abs(".")
		if err != nil {
			fakeToolErr = err
			return
		}
		dir, err := os.MkdirTemp("", "lstk-faketool-build-*")
		if err != nil {
			fakeToolErr = err
			return
		}
		out := filepath.Join(dir, execName("faketool"))
		cmd := exec.Command("go", "build", "-o", out, "./test-samples/faketool")
		cmd.Dir = moduleRoot
		if b, err := cmd.CombinedOutput(); err != nil {
			fakeToolErr = fmt.Errorf("build faketool: %w: %s", err, b)
			return
		}
		fakeToolPath = out
	})
	require.NoError(t, fakeToolErr)
	return fakeToolPath
}

// installFakeTool copies the faketool binary into dir under the given tool
// name (execName-suffixed on Windows) and writes its JSON config sidecar.
func installFakeTool(t *testing.T, dir, name string, cfg fakeToolConfig) {
	t.Helper()
	bin := filepath.Join(dir, execName(name))
	copyExecutable(t, fakeToolBinary(t), bin)
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(bin+".fakecfg", b, 0o644))
}

// writeFakeTool installs the faketool binary as `name` in a fresh temp dir
// with the given config, returning the dir (to prepend to PATH).
func writeFakeTool(t *testing.T, name string, cfg fakeToolConfig) string {
	t.Helper()
	dir := t.TempDir()
	installFakeTool(t, dir, name, cfg)
	return dir
}
