package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonInteractiveFlagBlocksLogin(t *testing.T) {
	t.Parallel()
	out, err := runLstkInPTY(t, testContext(t), testEnvWithHome(t.TempDir(), ""), "login", "--non-interactive")
	require.Error(t, err, "expected login --non-interactive to fail")
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "login requires an interactive terminal")
	assert.Contains(t, out, "LOCALSTACK_AUTH_TOKEN", "the error should name the non-interactive alternative")
	assert.Contains(t, out, agentsMDURL, "an agent hitting this error should be pointed at the agent setup guide")
}

func TestNonInteractiveLoginWhenAlreadyLoggedInShowsAgentGuide(t *testing.T) {
	t.Parallel()
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "fake-env-token")

	stdout, stderr, err := runLstk(t, testContext(t), "", e, "login", "--non-interactive")
	require.NoError(t, err, "login with a token already set should succeed non-interactively: %s", stderr)
	assert.Contains(t, stdout, "You're already logged in")
	assertAIAgentGuide(t, stdout)
}

func TestNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	out, err := runLstkInPTY(t, testContext(t), env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "start", "--non-interactive")
	require.Error(t, err, "expected start --non-interactive to fail with no auth token")
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode")
	assert.Contains(t, out, agentsMDURL, "an agent starting without a token should be pointed at the agent setup guide")
}

func TestRootNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	out, err := runLstkInPTY(t, testContext(t), env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "--non-interactive")
	require.Error(t, err, "expected lstk --non-interactive to fail with no auth token")
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode")
}
