package cmd

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realExitError produces a genuine *exec.ExitError with the given code, the
// same error shape awscli.Exec returns when a proxied tool exits non-zero.
func realExitError(t *testing.T, code int) error {
	t.Helper()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	} else {
		c = exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := c.Run()
	require.Error(t, err)
	return err
}

func TestExitCode(t *testing.T) {
	t.Run("nil error is 0", func(t *testing.T) {
		assert.Equal(t, 0, ExitCode(nil))
	})

	t.Run("plain error is 1", func(t *testing.T) {
		assert.Equal(t, 1, ExitCode(errors.New("boom")))
	})

	t.Run("proxied exit code unwraps through SilentError", func(t *testing.T) {
		err := output.NewSilentError(realExitError(t, 252))
		assert.Equal(t, 252, ExitCode(err))
	})

	t.Run("json envelope ExitCodeError code is used", func(t *testing.T) {
		err := output.NewSilentError(&output.ExitCodeError{Err: errors.New("confirmation required"), Code: 3})
		assert.Equal(t, 3, ExitCode(err))
	})
}

func TestProxySubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "service and operation",
			args: []string{"s3", "ls"},
			want: "s3 ls",
		},
		{
			name: "caps at two tokens so values are never recorded",
			args: []string{"s3", "cp", "file.txt", "s3://bucket"},
			want: "s3 cp",
		},
		{
			name: "single token",
			args: []string{"plan"},
			want: "plan",
		},
		{
			name: "empty args",
			args: nil,
			want: "",
		},
		{
			name: "leading double-dash flag stops collection so flag values are never recorded",
			args: []string{"--region", "us-east-1", "s3", "ls"},
			want: "",
		},
		{
			name: "single-dash flag stops collection",
			args: []string{"plan", "-json"},
			want: "plan",
		},
		{
			name: "lstk global flags are stripped first",
			args: []string{"--non-interactive", "s3", "ls"},
			want: "s3 ls",
		},
		{
			name: "overlong token is truncated",
			args: []string{strings.Repeat("a", 100), "ls"},
			want: strings.Repeat("a", 64) + " ls",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, proxySubcommand(tt.args))
		})
	}
}
