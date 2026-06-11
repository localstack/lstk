package caller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func classifyWith(env map[string]string, interactive bool) Classification {
	getenv := func(k string) string { return env[k] }
	return newClassifier(getenv, func() bool { return interactive }).Classify()
}

func TestClassify_Agents(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		identity string
	}{
		{"cursor", map[string]string{"CURSOR_TRACE_ID": "abc"}, "cursor"},
		{"cursor-cli", map[string]string{"CURSOR_AGENT": "1"}, "cursor-cli"},
		{"gemini", map[string]string{"GEMINI_CLI": "1"}, "gemini"},
		{"codex-sandbox", map[string]string{"CODEX_SANDBOX": "1"}, "codex"},
		{"codex-ci", map[string]string{"CODEX_CI": "1"}, "codex"},
		{"codex-thread", map[string]string{"CODEX_THREAD_ID": "t"}, "codex"},
		{"cowork", map[string]string{"CLAUDE_CODE_IS_COWORK": "1"}, "cowork"},
		{"claudecode", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"claude_code", map[string]string{"CLAUDE_CODE": "1"}, "claude-code"},
		{"copilot-model", map[string]string{"COPILOT_MODEL": "gpt-5"}, "github-copilot"},
		{"copilot-allow", map[string]string{"COPILOT_ALLOW_ALL": "true"}, "github-copilot"},
		{"copilot-token", map[string]string{"COPILOT_GITHUB_TOKEN": "tok"}, "github-copilot"},
		{"goose", map[string]string{"GOOSE_PROVIDER": "openai"}, "goose"},
		{"augment", map[string]string{"AUGMENT_AGENT": "1"}, "augment"},
		{"opencode", map[string]string{"OPENCODE": "1"}, "opencode"},
		{"opencode-caller", map[string]string{"OPENCODE_CALLER": "1"}, "opencode"},
		{"opencode-client", map[string]string{"OPENCODE_CLIENT": "1"}, "opencode"},
		{"antigravity", map[string]string{"ANTIGRAVITY_AGENT": "1"}, "antigravity"},
		{"devin-bashrc", map[string]string{"__COG_BASHRC_SOURCED": "1"}, "devin"},
		{"devin-shell", map[string]string{"__COG_SHELL_INTEGRATION_SCRIPT": "/p"}, "devin"},
		{"devin-pyenv", map[string]string{"__COG_SKIP_PYENV": "1"}, "devin"},
		{"replit", map[string]string{"REPL_ID": "r123"}, "replit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyWith(tc.env, true)
			assert.Equal(t, tc.identity, got.AgentIdentity)
			assert.True(t, got.IsAgent())
			assert.Equal(t, "agent", got.CallerType())
			assert.Equal(t, "agent_env", got.DetectionMethod())
		})
	}
}

func TestClassify_CoworkBeatsClaudeCode(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{"CLAUDE_CODE_IS_COWORK": "1", "CLAUDE_CODE": "1"}, true)
	assert.Equal(t, "cowork", got.AgentIdentity)
	assert.True(t, got.IsAgent())
}

func TestClassify_AgentAndCIAreOrthogonal(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{"CLAUDECODE": "1", "GITHUB_ACTIONS": "true", "CI": "true"}, false)
	assert.Equal(t, "claude-code", got.AgentIdentity, "the agent is recorded")
	assert.Equal(t, "github-actions", got.CIIdentity, "the CI host is recorded alongside the agent, not discarded")
	assert.True(t, got.IsAgent())
	assert.True(t, got.IsCI())
	assert.Equal(t, "agent", got.CallerType(), "agent takes precedence for single-label segmentation")
	assert.Equal(t, "agent_env", got.DetectionMethod())
}

func TestClassify_CISystems(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		identity string
	}{
		{"github-actions", map[string]string{"GITHUB_ACTIONS": "true"}, "github-actions"},
		{"gitlab-ci", map[string]string{"GITLAB_CI": "true"}, "gitlab-ci"},
		{"circleci", map[string]string{"CIRCLECI": "true"}, "circleci"},
		{"travis-ci", map[string]string{"TRAVIS": "true"}, "travis-ci"},
		{"jenkins", map[string]string{"JENKINS_URL": "http://ci"}, "jenkins"},
		{"buildkite", map[string]string{"BUILDKITE": "true"}, "buildkite"},
		{"azure-pipelines", map[string]string{"TF_BUILD": "True"}, "azure-pipelines"},
		{"bitbucket", map[string]string{"BITBUCKET_BUILD_NUMBER": "42"}, "bitbucket-pipelines"},
		{"teamcity", map[string]string{"TEAMCITY_VERSION": "2024"}, "teamcity"},
		{"appveyor", map[string]string{"APPVEYOR": "true"}, "appveyor"},
		{"drone", map[string]string{"DRONE": "true"}, "drone"},
		{"aws-codebuild", map[string]string{"CODEBUILD_BUILD_ID": "id"}, "aws-codebuild"},
		{"semaphore", map[string]string{"SEMAPHORE": "true"}, "semaphore"},
		{"generic-ci", map[string]string{"CI": "true"}, "ci"},
		{"continuous-integration", map[string]string{"CONTINUOUS_INTEGRATION": "true"}, "ci"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyWith(tc.env, false)
			assert.Equal(t, tc.identity, got.CIIdentity)
			assert.True(t, got.IsCI())
			assert.Empty(t, got.AgentIdentity)
			assert.Equal(t, "ci", got.CallerType())
			assert.Equal(t, "ci_env", got.DetectionMethod())
		})
	}
}

func TestClassify_SpecificCIBeatsGenericCI(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{"GITHUB_ACTIONS": "true", "CI": "true"}, false)
	assert.Equal(t, "github-actions", got.CIIdentity)
}

func TestClassify_HumanInteractive(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{}, true)
	assert.True(t, got.IsHuman())
	assert.Empty(t, got.AgentIdentity)
	assert.Empty(t, got.CIIdentity)
	assert.Equal(t, "human", got.CallerType())
	assert.Equal(t, "tty", got.DetectionMethod())
}

func TestClassify_HumanNonInteractive(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{"SOME_UNRELATED_VAR": "1"}, false)
	assert.True(t, got.IsHuman())
	assert.Empty(t, got.AgentIdentity)
	assert.Empty(t, got.CIIdentity)
	assert.Equal(t, "human", got.CallerType())
	assert.Equal(t, "no_tty", got.DetectionMethod())
}

func TestClassify_EmptyEnvValueIsNotSet(t *testing.T) {
	t.Parallel()
	got := classifyWith(map[string]string{"CLAUDECODE": ""}, true)
	assert.True(t, got.IsHuman())
	assert.Empty(t, got.AgentIdentity)
}
