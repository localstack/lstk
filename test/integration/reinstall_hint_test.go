package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildBundlingLstk builds lstk the way a bundling release does, stamped with
// version.bundlesExtensions=true, so the binary expects a bundle beside it.
func buildBundlingLstk(t *testing.T, ctx context.Context, version, outPath string) {
	t.Helper()
	buildLstkWithLdflags(t, ctx,
		versionLdflag(version)+" -X github.com/localstack/lstk/internal/version.bundlesExtensions=true", outPath)
}

func platformExe(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// writeBundledSet puts a stand-in bundled-extensions binary and descriptions
// file beside lstk, the layout a complete bundling install has.
func writeBundledSet(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, platformExe("bundled-extensions")), []byte("stand-in"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-extensions.toml"), []byte("doctor = \"Diagnose your setup\"\n"), 0o644))
}

func TestUnknownCommandHintsReinstallWhenBundleMissing(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	cases := []struct {
		name       string
		bundling   bool
		withBundle bool
		wantHint   bool
	}{
		{name: "bundling release without its bundle", bundling: true, wantHint: true},
		{name: "bundling release with its bundle", bundling: true, withBundle: true},
		{name: "pre-bundling release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			installDir := t.TempDir()
			lstk := filepath.Join(installDir, platformExe("lstk"))
			if tc.bundling {
				buildBundlingLstk(t, ctx, "0.0.2", lstk)
			} else {
				buildLstkWithVersion(t, ctx, "0.0.2", lstk)
			}
			if tc.withBundle {
				writeBundledSet(t, installDir)
			}

			cmd := exec.CommandContext(ctx, lstk, "nosuchcmd")
			cmd.Env = testEnvWithHome(t.TempDir(), "")
			out, err := cmd.CombinedOutput()
			require.Error(t, err, "an unknown command must still exit non-zero: %s", out)
			assert.Contains(t, string(out), `unknown command "nosuchcmd"`)

			if !tc.wantHint {
				assert.NotContains(t, string(out), "Reinstall lstk")
				return
			}
			resolvedDir, err := filepath.EvalSymlinks(installDir)
			require.NoError(t, err)
			assert.Contains(t, string(out), "bundled extensions")
			assert.Contains(t, string(out), resolvedDir)
			assert.Contains(t, string(out), "Reinstall lstk")
			assert.Contains(t, string(out), "https://github.com/localstack/lstk/releases")
		})
	}
}

func TestUpdateUpToDateWarnsWhenBundleMissing(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	srv := mockGitHubReleaseServer(t, "v0.0.2", nil)

	t.Run("bundle missing: plain and JSON output carry the warning", func(t *testing.T) {
		t.Parallel()
		installDir := t.TempDir()
		lstk := filepath.Join(installDir, platformExe("lstk"))
		buildBundlingLstk(t, ctx, "0.0.2", lstk)

		cmd := exec.CommandContext(ctx, lstk, "update", "--non-interactive")
		cmd.Env = mockGitHubEnv(t, srv)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "lstk update failed: %s", out)
		assert.Contains(t, string(out), "Already up to date")
		assert.Contains(t, string(out), "bundled extensions")
		assert.Contains(t, string(out), "Reinstall lstk")

		cmd = exec.CommandContext(ctx, lstk, "update", "--check", "--json")
		cmd.Env = mockGitHubEnv(t, srv)
		out, err = cmd.CombinedOutput()
		require.NoError(t, err, "lstk update --check --json failed: %s", out)
		var envelope struct {
			Warnings []struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"warnings"`
		}
		require.NoError(t, json.Unmarshal(out, &envelope), "not a JSON envelope: %s", out)
		require.Len(t, envelope.Warnings, 1)
		assert.Contains(t, envelope.Warnings[0].Message, "Reinstall lstk")
	})

	t.Run("bundle present: no warning", func(t *testing.T) {
		t.Parallel()
		installDir := t.TempDir()
		lstk := filepath.Join(installDir, platformExe("lstk"))
		buildBundlingLstk(t, ctx, "0.0.2", lstk)
		writeBundledSet(t, installDir)

		cmd := exec.CommandContext(ctx, lstk, "update", "--non-interactive")
		cmd.Env = mockGitHubEnv(t, srv)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "lstk update failed: %s", out)
		assert.Contains(t, string(out), "Already up to date")
		assert.NotContains(t, string(out), "Reinstall lstk")
	})
}
