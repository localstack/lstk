package container

import (
	"testing"

	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDockerFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		want        ParsedFlags
		errContains string
	}{
		{name: "-e", input: "-e FOO=bar", want: ParsedFlags{Env: []string{"FOO=bar"}}},
		{name: "--env", input: "--env SERVICES=s3,sqs", want: ParsedFlags{Env: []string{"SERVICES=s3,sqs"}}},
		{name: "--env=", input: "--env=DEBUG=1", want: ParsedFlags{Env: []string{"DEBUG=1"}}},
		{name: "-e inline", input: "-eSERVICES=s3", want: ParsedFlags{Env: []string{"SERVICES=s3"}}},
		{name: "-e quoted", input: `-e "FOO=hello world"`, want: ParsedFlags{Env: []string{"FOO=hello world"}}},

		{name: "-v", input: "-v /tmp/data:/data", want: ParsedFlags{Binds: []runtime.BindMount{{HostPath: "/tmp/data", ContainerPath: "/data"}}}},
		{name: "-v readonly", input: "-v /tmp/data:/data:ro", want: ParsedFlags{Binds: []runtime.BindMount{{HostPath: "/tmp/data", ContainerPath: "/data", ReadOnly: true}}}},
		{name: "--volume", input: "--volume /tmp/data:/data", want: ParsedFlags{Binds: []runtime.BindMount{{HostPath: "/tmp/data", ContainerPath: "/data"}}}},
		{name: "--volume=", input: "--volume=/tmp/data:/data", want: ParsedFlags{Binds: []runtime.BindMount{{HostPath: "/tmp/data", ContainerPath: "/data"}}}},

		{name: "multiple flags", input: "-e SERVICES=s3,sqs -v /tmp:/data", want: ParsedFlags{
			Env:   []string{"SERVICES=s3,sqs"},
			Binds: []runtime.BindMount{{HostPath: "/tmp", ContainerPath: "/data"}},
		}},
		{name: "empty", input: ""},

		{name: "unknown flag", input: "--rm", errContains: "unsupported docker flag"},
		{name: "--network unsupported", input: "--network host", errContains: "unsupported docker flag"},
		{name: "missing value", input: "-e", errContains: "requires a value"},
		{name: "unterminated quote", input: `-e "FOO=bar`, errContains: "unterminated quote"},
		{name: "invalid volume", input: "-v /nocolon", errContains: "invalid volume spec"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDockerFlags(tc.input)
			if tc.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
