package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzCommandFailsWhenAzureCLINotInstalled(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)

	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), workDir, e, "az", "group", "list")
	require.Error(t, err)
	assert.Contains(t, stdout, "az CLI not found in PATH")
	assert.Contains(t, stdout, "Install Azure CLI:")
	assert.Contains(t, stdout, "https://learn.microsoft.com/en-us/cli/azure/")
}
