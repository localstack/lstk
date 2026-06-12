package azurecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
func Exec(ctx context.Context, extraEnv []string, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
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
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if err := cmd.Run(); err != nil {
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

// Run executes `az <args...>` with extraEnv and returns the captured stdout, stderr,
// and any error. On non-zero exit, the error wraps stderr to aid debugging.
func Run(ctx context.Context, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	runErr := Exec(ctx, extraEnv, nil, &outBuf, &errBuf, args...)
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
