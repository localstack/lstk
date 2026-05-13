package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTerraformCommandCustomTFCmdEnv(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()
	customBin := "my-tofu"
	script := `#!/bin/sh
echo "INVOKED_AS:` + customBin + `"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, customBin), []byte(script), 0o755))

	e := env.With(env.DisableEvents, "1").
		With("PATH", dir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		With("TF_CMD", customBin)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "init")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)
	assert.Contains(t, stdout, "INVOKED_AS:"+customBin)
}

func TestTerraformCommandCustomizeAccessKey(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "AKIAEXAMPLE").
		With("CUSTOMIZE_ACCESS_KEY", "1")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "init")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)
	// The leading 'A' is rewritten to 'L' before terraform sees the env var.
	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=LKIAEXAMPLE")
}

func TestTerraformCommandDerivesS3EndpointFromLocalStackHost(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		With("LOCALSTACK_HOST", "localhost:4566")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)

	// localhost:4566 should derive to the wildcard subdomain S3 endpoint.
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL=http://localhost:4566")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_S3=http://s3.localhost.localstack.cloud:4566")
}
