package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	agentsMDURL         = "https://www.localstack.cloud/.well-known/agents.md"
	aiWorkflowsDocsURL  = "https://docs.localstack.cloud/aws/getting-started/ai-workflows/"
	aiAgentGuideHeading = "Build with your favorite AI agent"
)

func assertAIAgentGuide(t *testing.T, out string) {
	t.Helper()
	assert.Contains(t, out, aiAgentGuideHeading)
	assert.Contains(t, out, "• Paste this prompt into your agent:")
	assert.Contains(t, out, "Set up LocalStack using "+agentsMDURL)
	assert.Contains(t, out, "• Guide: "+aiWorkflowsDocsURL)
}

func TestStartSummaryPointsAtAgentsGuide(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	startTestContainer(t, testContext(t))

	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "fake-token")
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath,
		[]byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--non-interactive")
	require.NoError(t, err, "start against the running emulator should succeed: %s", stderr)
	assert.Contains(t, stdout, "• AI agents: "+agentsMDURL)
	assert.NotContains(t, stdout, aiAgentGuideHeading, "the login guide block is not part of the start summary")
}
