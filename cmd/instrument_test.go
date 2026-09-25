package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
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
		name    string
		command string
		args    []string
		want    string
	}{
		{
			name:    "aws service and operation",
			command: "aws",
			args:    []string{"s3", "ls"},
			want:    "s3 ls",
		},
		{
			name:    "aws caps at two tokens so values are never recorded",
			command: "aws",
			args:    []string{"s3", "cp", "file.txt", "s3://bucket"},
			want:    "s3 cp",
		},
		{
			name:    "terraform flat command records one token",
			command: "terraform",
			args:    []string{"plan"},
			want:    "plan",
		},
		{
			name:    "terraform positional address is not recorded",
			command: "terraform",
			args:    []string{"import", "aws_s3_bucket.customer", "bucket-name"},
			want:    "import",
		},
		{
			name:    "terraform nested command records two tokens",
			command: "terraform",
			args:    []string{"state", "rm", "aws_s3_bucket.customer"},
			want:    "state rm",
		},
		{
			name:    "cdk stack name is not recorded",
			command: "cdk",
			args:    []string{"deploy", "CustomerStack"},
			want:    "deploy",
		},
		{
			name:    "sam function name is not recorded",
			command: "sam",
			args:    []string{"build", "CustomerFunction"},
			want:    "build",
		},
		{
			name:    "sam nested command records two tokens",
			command: "sam",
			args:    []string{"local", "invoke", "CustomerFunction"},
			want:    "local invoke",
		},
		{
			name:    "az positional search term is not recorded",
			command: "az",
			args:    []string{"find", "customer name"},
			want:    "find",
		},
		{
			name:    "empty args",
			command: "aws",
			args:    nil,
			want:    "",
		},
		{
			name:    "leading double-dash flag stops collection so flag values are never recorded",
			command: "aws",
			args:    []string{"--region", "us-east-1", "s3", "ls"},
			want:    "",
		},
		{
			name:    "single-dash flag stops collection",
			command: "terraform",
			args:    []string{"plan", "-json"},
			want:    "plan",
		},
		{
			name:    "lstk global flags are stripped first",
			command: "aws",
			args:    []string{"--non-interactive", "s3", "ls"},
			want:    "s3 ls",
		},
		{
			name:    "overlong token is truncated",
			command: "aws",
			args:    []string{strings.Repeat("a", 100), "ls"},
			want:    strings.Repeat("a", 64) + " ls",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, proxySubcommand(tt.command, tt.args))
		})
	}
}

func TestCommandInstrumentationRecordsFinalJSONExitCode(t *testing.T) {
	events := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			Events []map[string]any `json:"events"`
		}
		if !assert.NoError(t, json.Unmarshal(body, &request)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, event := range request.Events {
			events <- event
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &env.Env{JSON: true}
	tel := telemetry.NewWithInProcessFlush(srv.URL)
	root := &cobra.Command{Use: "lstk", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(&cobra.Command{
		Use:         "confirm",
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			sink := jsonAwareSink(cmd, cfg, io.Discard)
			sink.Emit(output.ErrorEvent{
				Title: "confirmation required",
				Code:  output.ErrConfirmationRequired,
			})
			return output.NewSilentError(errors.New("confirmation required"))
		},
	})

	var stdout bytes.Buffer
	configureCommandExecution(root, cfg, tel, &stdout)
	root.SetArgs([]string{"confirm"})
	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Equal(t, 3, ExitCode(err))

	tel.Close()
	select {
	case event := <-events:
		payload, ok := event["payload"].(map[string]any)
		require.True(t, ok)
		result, ok := payload["result"].(map[string]any)
		require.True(t, ok)
		assert.InDelta(t, 3, result["exit_code"], 0)
	default:
		t.Fatal("no telemetry event received")
	}
}

// The pipe derives `proxied` from this set for rows older than the field, and
// the failure-rate denominator depends on it going forward. A sixth proxy
// command must update this list and the analytics contract in
// openspec/changes/distinguish-proxied-command-errors/design.md.
func TestProxyCommandAnnotationSetIsExactlyTheDocumentedProxies(t *testing.T) {
	root := NewRootCmd(&env.Env{}, telemetry.New("", true), log.Nop())

	var annotated []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if _, ok := c.Annotations[proxyCommandAnnotation]; ok {
			annotated = append(annotated, c.CommandPath())
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(root)
	sort.Strings(annotated)

	assert.Equal(t, []string{"lstk aws", "lstk az", "lstk cdk", "lstk sam", "lstk terraform"}, annotated)
}

// The annotation marks a command; `proxied` describes an invocation. The
// interception subcommands and `setup azure` (alias `az`) do lstk's own work
// and must not carry it.
func TestProxyCommandAnnotationIsNotInherited(t *testing.T) {
	root := NewRootCmd(&env.Env{}, telemetry.New("", true), log.Nop())

	for _, path := range [][]string{{"az", "start-interception"}, {"az", "stop-interception"}, {"setup", "azure"}} {
		c, _, err := root.Find(path)
		require.NoError(t, err, path)
		_, ok := c.Annotations[proxyCommandAnnotation]
		assert.False(t, ok, "%v must not be annotated", path)
	}
}

func TestCommandResult(t *testing.T) {
	live := context.Background()
	done, cancel := context.WithCancel(context.Background())
	cancel()

	intp := func(v int) *int { return &v }

	tests := []struct {
		name    string
		ctx     context.Context
		err     error
		proxied bool
		want    telemetry.CommandResult
	}{
		{
			name: "lstk command succeeds",
			ctx:  live, err: nil, proxied: false,
			want: telemetry.CommandResult{ExitCode: 0},
		},
		{
			name: "lstk command fails",
			ctx:  live, err: errors.New("port 4566 already in use"), proxied: false,
			want: telemetry.CommandResult{ExitCode: 1, ErrorMsg: "port 4566 already in use"},
		},
		{
			// A proxied RunE returns nil only through its exec site, so nil
			// means the tool exited 0.
			name: "proxied tool succeeds",
			ctx:  live, err: nil, proxied: true,
			want: telemetry.CommandResult{ExitCode: 0, ProxyExitCode: intp(0)},
		},
		{
			name: "proxied tool fails with its own code",
			ctx:  live, err: output.NewSilentError(proc.MarkUserToolExit(realExitError(t, 252))), proxied: true,
			want: telemetry.CommandResult{ExitCode: 252, ErrorMsg: "exit status 252", ProxyExitCode: intp(252)},
		},
		{
			// lstk's own subprocess failed (a version probe, S3 provisioning):
			// the code propagates, but no wrapped tool ran.
			name: "proxied invocation fails on an unmarked child exit",
			ctx:  live, err: realExitError(t, 2), proxied: true,
			want: telemetry.CommandResult{ExitCode: 2, ErrorMsg: "exit status 2"},
		},
		{
			name: "proxied invocation fails preflight",
			ctx:  live, err: output.NewSilentError(errors.New("runtime not healthy")), proxied: true,
			want: telemetry.CommandResult{ExitCode: 1, ErrorMsg: "runtime not healthy"},
		},
		{
			// The tool trapped the signal and exited normally; only lstk's
			// context knows it was interrupted.
			name: "signalled proxied tool that exited normally",
			ctx:  done, err: output.NewSilentError(proc.MarkUserToolExit(realExitError(t, 130))), proxied: true,
			want: telemetry.CommandResult{ExitCode: 130, ErrorMsg: "exit status 130", ProxyExitCode: intp(130), Cancelled: true},
		},
		{
			name: "signalled lstk command",
			ctx:  done, err: output.NewSilentError(context.Canceled), proxied: false,
			want: telemetry.CommandResult{ExitCode: 1, ErrorMsg: "context canceled", Cancelled: true},
		},
		{
			// TUI `q` cancels the run context without a signal.
			name: "TUI quit without a signal",
			ctx:  live, err: output.NewSilentError(context.Canceled), proxied: false,
			want: telemetry.CommandResult{ExitCode: 1, ErrorMsg: "context canceled", Cancelled: true},
		},
		{
			// Raw: the signal is recorded although the command finished. The
			// pipe reads exit_code = 0 as success.
			name: "signal arrives as the command finishes",
			ctx:  done, err: nil, proxied: false,
			want: telemetry.CommandResult{ExitCode: 0, Cancelled: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := commandResult(tc.ctx, tc.err, tc.proxied)
			assert.Equal(t, tc.want, got)
		})
	}
}
