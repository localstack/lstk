package caller

import (
	"os"

	"golang.org/x/term"
)

type Type string

const (
	TypeHuman Type = "human"
	TypeAgent Type = "agent"
	TypeCI    Type = "ci"
)

const (
	MethodAgentEnv = "agent_env"
	MethodCIEnv    = "ci_env"
	MethodTTY      = "tty"
	MethodNoTTY    = "no_tty"
)

type Classification struct {
	Type     Type
	Identity string
	Method   string
}

type detector struct {
	identity string
	envVars  []string
}

func agentDetectors() []detector {
	return []detector{
		{"cursor", []string{"CURSOR_TRACE_ID"}},
		{"cursor-cli", []string{"CURSOR_AGENT"}},
		{"gemini", []string{"GEMINI_CLI"}},
		{"codex", []string{"CODEX_SANDBOX", "CODEX_CI", "CODEX_THREAD_ID"}},
		{"cowork", []string{"CLAUDE_CODE_IS_COWORK"}},
		{"claude-code", []string{"CLAUDECODE", "CLAUDE_CODE"}},
		{"github-copilot", []string{"COPILOT_MODEL", "COPILOT_ALLOW_ALL", "COPILOT_GITHUB_TOKEN"}},
		{"goose", []string{"GOOSE_PROVIDER"}},
		{"augment", []string{"AUGMENT_AGENT"}},
		{"opencode", []string{"OPENCODE", "OPENCODE_CALLER", "OPENCODE_CLIENT"}},
		{"antigravity", []string{"ANTIGRAVITY_AGENT"}},
		{"devin", []string{"__COG_BASHRC_SOURCED", "__COG_SHELL_INTEGRATION_SCRIPT", "__COG_SKIP_PYENV"}},
		{"replit", []string{"REPL_ID"}},
	}
}

func ciDetectors() []detector {
	return []detector{
		{"github-actions", []string{"GITHUB_ACTIONS"}},
		{"gitlab-ci", []string{"GITLAB_CI"}},
		{"circleci", []string{"CIRCLECI"}},
		{"travis-ci", []string{"TRAVIS"}},
		{"jenkins", []string{"JENKINS_URL"}},
		{"buildkite", []string{"BUILDKITE"}},
		{"azure-pipelines", []string{"TF_BUILD"}},
		{"bitbucket-pipelines", []string{"BITBUCKET_BUILD_NUMBER"}},
		{"teamcity", []string{"TEAMCITY_VERSION"}},
		{"appveyor", []string{"APPVEYOR"}},
		{"drone", []string{"DRONE"}},
		{"aws-codebuild", []string{"CODEBUILD_BUILD_ID"}},
		{"semaphore", []string{"SEMAPHORE"}},
		{"ci", []string{"CI", "CONTINUOUS_INTEGRATION"}},
	}
}

type Classifier struct {
	agentDetectors []detector
	ciDetectors    []detector
	getenv         func(string) string
	isInteractive  func() bool
}

func New() *Classifier {
	return newClassifier(os.Getenv, stdinStdoutAreTerminal)
}

func newClassifier(getenv func(string) string, isInteractive func() bool) *Classifier {
	return &Classifier{
		agentDetectors: agentDetectors(),
		ciDetectors:    ciDetectors(),
		getenv:         getenv,
		isInteractive:  isInteractive,
	}
}

func (c *Classifier) Classify() Classification {
	if id := match(c.agentDetectors, c.getenv); id != "" {
		return Classification{Type: TypeAgent, Identity: id, Method: MethodAgentEnv}
	}
	if id := match(c.ciDetectors, c.getenv); id != "" {
		return Classification{Type: TypeCI, Identity: id, Method: MethodCIEnv}
	}
	if c.isInteractive() {
		return Classification{Type: TypeHuman, Method: MethodTTY}
	}
	return Classification{Type: TypeHuman, Method: MethodNoTTY}
}

func match(detectors []detector, getenv func(string) string) string {
	for _, d := range detectors {
		for _, v := range d.envVars {
			if getenv(v) != "" {
				return d.identity
			}
		}
	}
	return ""
}

func stdinStdoutAreTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
