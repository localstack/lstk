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
	Pager         bool           `json:"pager,omitempty"`
	ExitCode      int            `json:"exitCode,omitempty"`
}

var (
	fakeToolOnce sync.Once
	fakeToolDir  string
	fakeToolPath string
	fakeToolErr  error
)

// buildFakeTool builds test-samples/faketool once and returns the path to the
// compiled binary, following the referenceExtensionBinary pattern. It returns
// its error because TestMain calls it before any *testing.T exists.
func buildFakeTool() (string, error) {
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
		fakeToolDir = dir
		out := filepath.Join(dir, execName("faketool"))
		cmd := exec.Command("go", "build", "-o", out, "./test-samples/faketool")
		cmd.Dir = moduleRoot
		if b, err := cmd.CombinedOutput(); err != nil {
			fakeToolErr = fmt.Errorf("build faketool: %w: %s", err, b)
			return
		}
		fakeToolPath = out
	})
	return fakeToolPath, fakeToolErr
}

// cleanupFakeToolBuild removes buildFakeTool's build directory after m.Run,
// including one left behind by a failed build.
func cleanupFakeToolBuild() {
	if fakeToolDir == "" {
		return
	}
	_ = os.RemoveAll(fakeToolDir)
}

// installFakeToolAt copies the faketool binary into dir under the given tool
// name (execName-suffixed on Windows), writes its JSON config sidecar and
// returns the installed path. It is the *testing.T-free variant, for TestMain.
func installFakeToolAt(dir, name string, cfg fakeToolConfig) (string, error) {
	src, err := buildFakeTool()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, execName(name))
	if err := copyExecutableTo(src, bin); err != nil {
		return "", err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(bin+".fakecfg", b, 0o644); err != nil {
		return "", err
	}
	return bin, nil
}

// installFakeTool copies the faketool binary into dir under the given tool
// name (execName-suffixed on Windows) and writes its JSON config sidecar.
func installFakeTool(t *testing.T, dir, name string, cfg fakeToolConfig) {
	t.Helper()
	_, err := installFakeToolAt(dir, name, cfg)
	require.NoError(t, err)
}

// writeFakeTool installs the faketool binary as `name` in a fresh temp dir
// with the given config, returning the dir (to prepend to PATH).
func writeFakeTool(t *testing.T, name string, cfg fakeToolConfig) string {
	t.Helper()
	dir := t.TempDir()
	installFakeTool(t, dir, name, cfg)
	return dir
}
