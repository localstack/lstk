package extension

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

// Invoke executes the resolved extension with args forwarded unmodified,
// connecting the child's stdin/stdout/stderr to lstk's own so the user's
// terminal is wired straight through. The runtime context is layered on the
// inherited host environment.
//
// The invocation is wrapped in an OpenTelemetry span recording the extension
// name, whether it was bundled, and the exit code, so extension usage is visible
// when telemetry is enabled (LSTK_OTEL); when telemetry is disabled the global
// no-op tracer makes this free and emits nothing. lstk does not inject trace
// context into the extension process, so an extension's own spans do not yet
// nest under lstk's trace (deferred).
//
// A non-zero exit from the extension is wrapped as a silent error carrying the
// *exec.ExitError, so the top-level handler propagates the child's exit code as
// lstk's own (via main.go's errors.As check) without printing an extra
// lstk-level error line over the extension's output. Modelled on the IaC
// proxies' exec path.
func Invoke(ctx context.Context, ext *Extension, args []string, runCtx Context) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/extension").Start(ctx, "extension")
	defer span.End()
	span.SetAttributes(
		attribute.String("extension.name", ext.Name),
		attribute.Bool("extension.bundled", ext.Bundled),
	)

	envv, err := runCtx.Environ(os.Environ())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	cmd := exec.CommandContext(ctx, ext.Path, args...)
	cmd.Env = envv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := proc.Run(cmd); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("extension.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "extension exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("extension.exit_code", 0))
	return nil
}
