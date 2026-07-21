package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

// Run proxies an AWS CDK invocation against LocalStack. It locates the cdk
// binary, verifies its version, builds a subprocess environment that points CDK
// at the resolved LocalStack endpoint (and strips ambient AWS config that could
// redirect it at real AWS), then runs cdk with stdio wired through.
//
// endpointURL is the resolved LocalStack endpoint (http://host:port). region is
// encoded into the subprocess environment as AWS_REGION. CDK has no account
// selection: it always targets the default LocalStack account via a fixed mock
// AWS_ACCESS_KEY_ID. CDK output is streamed unobstructed (no spinner); a
// non-zero exit is wrapped as a silent error so lstk does not reprint it.
func Run(ctx context.Context, endpointURL, region string, sink output.Sink, logger log.Logger, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/iac/cdk/cli").Start(ctx, "cdk cli")
	defer span.End()

	cdkBin, err := exec.LookPath(cdkCmd())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("%s not found in PATH", cdkCmd()),
			Actions: []output.ErrorAction{{Label: "Install CDK CLI:", Value: "npm install -g aws-cdk"}},
		})
		return output.NewSilentError(fmt.Errorf("%s not found in PATH", cdkCmd()))
	}

	if err := CheckVersion(ctx, cdkBin); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		sink.Emit(output.ErrorEvent{
			Title:   err.Error(),
			Actions: []output.ErrorAction{{Label: "Upgrade CDK CLI:", Value: "npm install -g aws-cdk@latest"}},
		})
		return output.NewSilentError(err)
	}

	effectiveEndpoint := endpointURL
	if override := endpointURLOverride(); override != "" {
		effectiveEndpoint = override
		logger.Info("cdk: using AWS_ENDPOINT_URL override %s", override)
	}

	_, s3Endpoint := endpoint.S3Addressing(effectiveEndpoint)
	if override := s3EndpointOverride(); override != "" {
		s3Endpoint = override
		logger.Info("cdk: using AWS_ENDPOINT_URL_S3 override %s", override)
	}

	span.SetAttributes(
		attribute.StringSlice("cdk.args", args),
		attribute.Bool("cdk.offline", IsOffline(args)),
	)

	cmd := exec.CommandContext(ctx, cdkBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = BuildEnv(os.Environ(), effectiveEndpoint, s3Endpoint, region)

	if err := proc.Run(cmd); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("cdk.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "cdk exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// strippedKeys are ambient AWS configuration variables removed from the CDK
// subprocess environment. A named profile, default profile, or stale session
// token could otherwise resolve real credentials/region and silently redirect a
// deploy at real AWS — this mirrors why cdklocal clears AWS config before
// invoking cdk.
var strippedKeys = map[string]bool{
	"AWS_PROFILE":         true,
	"AWS_DEFAULT_PROFILE": true,
	"AWS_SESSION_TOKEN":   true,
}

// BuildEnv returns the environment for the cdk subprocess: base with ambient
// AWS configuration stripped and the LocalStack-pointing values set (overriding
// any pre-existing entries). Empty endpoint values are not set, so they never
// clobber a meaningful inherited value with "".
//
// AWS_ACCESS_KEY_ID is fixed to the mock value "test" (never an account id), so
// CDK always resolves the default LocalStack account 000000000000. This
// unconditionally overrides any ambient AWS_ACCESS_KEY_ID — including a 12-digit
// value that LocalStack would otherwise treat as a custom account.
func BuildEnv(base []string, endpointURL, s3Endpoint, region string) []string {
	// Ordered so the produced environment is deterministic. Empty-valued
	// entries are skipped below.
	managed := []struct{ key, value string }{
		{"AWS_ENDPOINT_URL", endpointURL},
		{"AWS_ENDPOINT_URL_S3", s3Endpoint},
		{"AWS_ACCESS_KEY_ID", "test"},
		{"AWS_SECRET_ACCESS_KEY", "test"},
		{"AWS_REGION", region},
		{"AWS_DEFAULT_REGION", region},
		{"CDK_DISABLE_LEGACY_EXPORT_WARNING", "1"},
	}

	managedKeys := make(map[string]bool, len(managed))
	for _, m := range managed {
		managedKeys[m.key] = true
	}

	env := make([]string, 0, len(base)+len(managed))
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			env = append(env, e)
			continue
		}
		if strippedKeys[key] || managedKeys[key] {
			continue
		}
		env = append(env, e)
	}
	for _, m := range managed {
		if m.value == "" {
			continue
		}
		env = append(env, m.key+"="+m.value)
	}
	return env
}
