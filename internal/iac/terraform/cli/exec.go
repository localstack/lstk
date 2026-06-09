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
	"go.opentelemetry.io/otel/trace"

	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
)

// Run proxies a terraform invocation against LocalStack. For proxied commands
// it discovers the AWS provider's endpoint keys from the schema, generates a
// provider-override file pointing every aws provider/alias block at the
// resolved endpoint, runs terraform, and removes the generated file afterward.
// Unproxied subcommands (fmt/validate/version) run terraform directly without
// an override and do not require a resolved endpoint.
//
// endpointURL is the resolved LocalStack endpoint (http://host:port); it may be
// empty for unproxied commands. region and account are encoded into each
// generated provider block.
func Run(ctx context.Context, endpointURL, region, account string, sink output.Sink, logger log.Logger, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/iac/terraform/cli").Start(ctx, "terraform cli")
	defer span.End()

	tfBin, err := exec.LookPath(tfCmd())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		installURL := "https://developer.hashicorp.com/terraform/install"
		if tfCmd() == "tofu" {
			installURL = "https://opentofu.org/docs/intro/install/"
		}
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("%s not found in PATH", tfCmd()),
			Actions: []output.ErrorAction{{Label: "Install it and ensure it is on your PATH:", Value: installURL}},
		})
		return output.NewSilentError(fmt.Errorf("%s not found in PATH", tfCmd()))
	}
	span.SetAttributes(attribute.StringSlice("terraform.args", args), attribute.Bool("terraform.unproxied", IsUnproxied(args)))

	if IsUnproxied(args) {
		return runTerraform(ctx, span, tfBin, args)
	}

	workdir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}

	endpoint := endpointURL
	if override := endpointURLOverride(); override != "" {
		endpoint = override
		logger.Info("terraform: using AWS_ENDPOINT_URL override %s", override)
	}

	keys, err := EndpointKeys(ctx, tfBin, workdir)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if errors.Is(err, ErrInitRequired) {
			sink.Emit(output.ErrorEvent{
				Title:   "Terraform AWS provider is not installed",
				Actions: []output.ErrorAction{{Label: "Initialize the project:", Value: tfCmd() + " init"}},
			})
			return output.NewSilentError(err)
		}
		return err
	}
	logger.Info("terraform: discovered %d endpoint keys from provider schema", len(keys))

	written, err := generateOverride(overrideOptions{
		workdir:      workdir,
		fileName:     overrideFileName(),
		endpointURL:  endpoint,
		region:       region,
		account:      account,
		endpointKeys: keys,
		logger:       logger,
	})
	if err != nil {
		return err
	}

	if dryRun() {
		// Leave the override in place for inspection and do not run terraform.
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     fmt.Sprintf("LSTK_TF_DRY_RUN: generated %s and skipped terraform", written[0]),
		})
		return nil
	}

	defer func() {
		for _, p := range written {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				logger.Error("terraform: failed to remove generated override %s: %v", p, err)
			}
		}
	}()

	return runTerraform(ctx, span, tfBin, args)
}

// runTerraform executes terraform with stdio wired through to the user and
// propagates the exit code. A non-zero exit is wrapped as a silent error so
// lstk does not print an additional error line over terraform's own output.
func runTerraform(ctx context.Context, span trace.Span, tfBin string, args []string) error {
	cmd := exec.CommandContext(ctx, tfBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			span.SetAttributes(attribute.Int("terraform.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "terraform exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
