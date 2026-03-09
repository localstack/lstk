package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonInteractiveFlagBlocksLogin(t *testing.T) {
	_, stderr, err := runLstk(t, testContext(t), "", nil, "login", "--non-interactive")
	require.Error(t, err, "expected login --non-interactive to fail")
	assert.Contains(t, stderr, "login requires an interactive terminal")
}

func TestNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	_, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "start", "--non-interactive")
	require.Error(t, err, "expected start --non-interactive to fail with no auth token")
	assert.Contains(t, stderr, "LOCALSTACK_AUTH_TOKEN")
}

func TestRootNonInteractiveFlagFailsWithoutToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	_, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "--non-interactive")
	require.Error(t, err, "expected lstk --non-interactive to fail with no auth token")
	assert.Contains(t, stderr, "LOCALSTACK_AUTH_TOKEN")
}
