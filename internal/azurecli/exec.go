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

// ErrNotInstalled is returned when the `az` binary cannot be found on PATH.
var ErrNotInstalled = errors.New("az CLI not found in PATH — install it from https://learn.microsoft.com/cli/azure/install-azure-cli")

// Exec runs `az <args...>` with the given stdout/stderr writers, inheriting stdin.
func Exec(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
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
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
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

// Run executes `az <args...>` and returns the captured stdout, stderr, and any error.
// On non-zero exit, the error wraps stderr to aid debugging.
func Run(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	runErr := Exec(ctx, &outBuf, &errBuf, args...)
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
