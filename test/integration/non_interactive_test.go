package integration_test

import (
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
)

func TestNonInteractiveFlagBlocksLogin(t *testing.T) {
	t.Parallel()
	out, err := runLstkInPTY(t, testContext(t), testEnvWithHome(t.TempDir(), ""), "login", "--non-interactive")
	must.Error(t, err, "expected login --non-interactive to fail")
	requireExitCode(t, 1, err)
	must.Contains(t, out, "login requires an interactive terminal")
}

func TestNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	out, err := runLstkInPTY(t, testContext(t), env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "start", "--non-interactive")
	must.Error(t, err, "expected start --non-interactive to fail with no auth token")
	requireExitCode(t, 1, err)
	must.Contains(t, out, "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode")
}

func TestRootNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	out, err := runLstkInPTY(t, testContext(t), env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "--non-interactive")
	must.Error(t, err, "expected lstk --non-interactive to fail with no auth token")
	requireExitCode(t, 1, err)
	must.Contains(t, out, "authentication required: set LOCALSTACK_AUTH_TOKEN or run in interactive mode")
}
