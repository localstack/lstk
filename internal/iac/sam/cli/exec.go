package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

const installDocsURL = "https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/install-sam-cli.html"

// Run proxies an AWS SAM CLI invocation against LocalStack. It locates the sam
// binary, verifies its version, builds a subprocess environment that points SAM
// at the resolved LocalStack endpoint (and strips ambient AWS config that could
// redirect it at real AWS), then runs sam with stdio wired through.
//
// endpointURL is the resolved LocalStack endpoint (http://host:port). account is
// written to AWS_ACCESS_KEY_ID (LocalStack derives the account from it) and
// region to AWS_REGION/AWS_DEFAULT_REGION. SAM output is streamed unobstructed
// (no spinner); a non-zero exit is wrapped as a silent error so lstk does not
// reprint it.
func Run(ctx context.Context, endpointURL, account, region string, sink output.Sink, logger log.Logger, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/iac/sam/cli").Start(ctx, "sam cli")
	defer span.End()

	samBin, err := exec.LookPath(samCmd())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("%s not found in PATH", samCmd()),
			Actions: []output.ErrorAction{{Label: "Install AWS SAM CLI:", Value: installDocsURL}},
		})
		return output.NewSilentError(fmt.Errorf("%s not found in PATH", samCmd()))
	}

	if err := CheckVersion(ctx, samBin); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		sink.Emit(output.ErrorEvent{
			Title:   err.Error(),
			Actions: []output.ErrorAction{{Label: "Upgrade AWS SAM CLI:", Value: installDocsURL}},
		})
		return output.NewSilentError(err)
	}

	effectiveEndpoint := endpointURL
	if override := endpointURLOverride(); override != "" {
		effectiveEndpoint = override
		logger.Info("sam: using AWS_ENDPOINT_URL override %s", override)
	}

	span.SetAttributes(
		attribute.StringSlice("sam.args", args),
		attribute.Bool("sam.offline", IsOffline(args)),
	)

	cmd := exec.CommandContext(ctx, samBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = BuildEnv(os.Environ(), effectiveEndpoint, account, region)

	if err := proc.Run(cmd); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("sam.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "sam exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
