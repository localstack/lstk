package azurecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/proc"
)

const InstallURL = "https://learn.microsoft.com/en-us/cli/azure/"

// ErrNotInstalled is returned when the `az` binary cannot be found on PATH.
var ErrNotInstalled = fmt.Errorf("az CLI not found in PATH — install it from %s", InstallURL)

// CheckInstalled returns ErrNotInstalled if the `az` binary is not on PATH.
// Callers should use this before performing setup work to avoid leaving partial state.
func CheckInstalled() error {
	if _, err := exec.LookPath("az"); err != nil {
		return ErrNotInstalled
	}
	return nil
}

// Exec runs `az <args...>`. extraEnv is appended to the inherited process environment
// (later entries win), letting callers inject AZURE_CONFIG_DIR, proxy, and CA settings
// without mutating the user's global Azure CLI configuration.
//
// When usePTY is true (lstk's stdout and stderr are both terminals), the child's
// output goes through a pseudo-terminal merged into stdout — see proc.RunInPTY
// for why; otherwise stdout/stderr are wired as given.
func Exec(ctx context.Context, extraEnv []string, usePTY bool, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azurecli").Start(ctx, "az cli")
	defer span.End()

	azBin, err := exec.LookPath("az")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ErrNotInstalled
	}

	span.SetAttributes(attribute.StringSlice("az.args", args))

	cmd := exec.CommandContext(ctx, azBin, args...)
	cmd.Stdin = stdin
	cmd.Env = execEnv(os.Environ(), extraEnv)

	var runErr error
	started := false
	if usePTY {
		started, runErr = proc.RunInPTY(cmd, stdout)
	}
	if !started {
		// No PTY requested or none could be allocated (e.g. on Windows): plain
		// writer wiring, as before.
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		runErr = proc.Run(cmd)
	}

	if err := runErr; err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("az.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "az cli exited non-zero")
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
	return nil
}

// execEnv builds the child environment for the Azure CLI: the inherited
// environment, then extraEnv (later entries win), then PYTHONUNBUFFERED.
//
// The Azure CLI is a Python program, so it block-buffers stdout when it gets a
// pipe instead of a terminal, holding back streaming output until exit
// (DEVX-1028, the `lstk az` twin of `lstk aws`'s DEVX-1026). Unlike the frozen
// aws v2 binary, every az distribution runs a real python interpreter, so
// PYTHONUNBUFFERED takes effect here; the PTY in Exec additionally lets az see
// a terminal, so its own terminal-gated output (progress reporting, colors)
// behaves as it does under a plain `az`. (Python treats any non-empty value as
// enabled, so a user-set value is left alone.)
func execEnv(base, extraEnv []string) []string {
	env := make([]string, 0, len(base)+len(extraEnv)+1)
	env = append(env, base...)
	env = append(env, extraEnv...)
	setIfAbsent(&env, "PYTHONUNBUFFERED", "1")
	return env
}

func setIfAbsent(env *[]string, key, value string) {
	prefix := key + "="
	for _, e := range *env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	*env = append(*env, prefix+value)
}

// Run executes `az <args...>` with extraEnv and returns the captured stdout, stderr,
// and any error. On non-zero exit, the error wraps stderr to aid debugging.
func Run(ctx context.Context, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	runErr := Exec(ctx, extraEnv, false, nil, &outBuf, &errBuf, args...)
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && stderr != "" {
			return stdout, stderr, fmt.Errorf("az %v: %w: %s", args, runErr, stderr)
		}
		return stdout, stderr, runErr
	}
	return stdout, stderr, nil
}
