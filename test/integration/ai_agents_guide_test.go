package integration_test

import (
	"encoding/json"
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

	stdout, stderr, err = runLstk(t, testContext(t), t.TempDir(), e, "start", "--non-interactive", "--json")
	require.NoError(t, err, "start --json against the running emulator should succeed: %s", stderr)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %s", stdout)
	assert.Equal(t, agentsMDURL, envelope.Data["agentsGuide"], "--json drops the summary lines, so the guide must be a data field")
}

func TestStatusPointsAtAgentsGuide(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	defer srv.Close()

	e := env.With(env.DisableEvents, "1").WithHome(t.TempDir())
	e = append(e, unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--endpoint-url", srv.URL, "status")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "• AI agents: "+agentsMDURL)
}

func TestRootHelpPointsAtAgentsGuide(t *testing.T) {
	t.Parallel()
	e := testEnvWithHome(t.TempDir(), "")

	stdout, _, err := runLstk(t, testContext(t), "", e, "--help")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Learn more:\n  Documentation  https://docs.localstack.cloud/aws/developer-tools/running-localstack/lstk/\n  AI agents      "+agentsMDURL)

	stdout, _, err = runLstk(t, testContext(t), "", e, "start", "--help")
	require.NoError(t, err)
	assert.NotContains(t, stdout, agentsMDURL, "only the root help carries the pointer")
}
