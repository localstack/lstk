package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/localstack/lstk/internal/awscli"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

// Run proxies a terraform invocation against LocalStack. The path taken depends
// on the subcommand and on whether the working directory declares an S3 backend:
//
//   - fmt/validate/version: run terraform directly; no override, no endpoint.
//   - init without an S3 backend: pass through directly so it can install the
//     provider (the override's endpoint keys are discovered from the provider
//     schema, which does not exist until init has run).
//   - init with an S3 backend: generate a backend-only override (no provider
//     blocks, since the schema is not yet available), provision the state bucket
//     and lock table, then run init.
//   - plan/apply/…: discover the provider endpoint keys from the schema and
//     generate a full override (provider blocks + backend + remote-state),
//     provision the backend when present, then run terraform.
//
// In every proxied case the generated override is removed afterward.
//
// endpointURL is the resolved LocalStack endpoint (http://host:port); it may be
// empty for the unproxied and backend-less init paths. region and account are
// encoded into each generated block. chdir is terraform's -chdir=DIR value ("" when
// absent); when set, lstk anchors all of its directory-relative work (schema
// discovery, override generation, cleanup) to that directory rather than the
// process working directory, mirroring the switch terraform itself makes.
func Run(ctx context.Context, endpointURL, region, account, chdir string, sink output.Sink, logger log.Logger, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/iac/terraform/cli").Start(ctx, "terraform cli")
	defer span.End()

	tfBin, err := exec.LookPath(tfCmd())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		installLabel, installURL := "Install Terraform CLI:", "https://developer.hashicorp.com/terraform/cli"
		if tfCmd() == "tofu" {
			installLabel, installURL = "Install OpenTofu CLI:", "https://opentofu.org/docs/intro/install/"
		}
		sink.Emit(output.ErrorEvent{
			Title:   fmt.Sprintf("%s not found in PATH", tfCmd()),
			Actions: []output.ErrorAction{{Label: installLabel, Value: installURL}},
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
	if chdir != "" {
		workdir = ResolveChdir(workdir, chdir)
		if info, statErr := os.Stat(workdir); statErr != nil || !info.IsDir() {
			sink.Emit(output.ErrorEvent{
				Title: fmt.Sprintf("-chdir directory does not exist: %s", chdir),
			})
			return output.NewSilentError(fmt.Errorf("-chdir directory does not exist: %s", workdir))
		}
	}

	isInit := subcommand(args) == "init"
	backend := parseS3Backend(workdir, logger)

	// init without an S3 backend passes through to bootstrap the provider.
	if isInit && backend == nil {
		return runTerraform(ctx, span, tfBin, args)
	}

	resolvedEndpoint := endpointURL
	if override := endpointURLOverride(); override != "" {
		resolvedEndpoint = override
		logger.Info("terraform: using AWS_ENDPOINT_URL override %s", override)
	}

	form := endpointForm{region: region, account: account}
	form.pathStyle, form.s3Endpoint = endpoint.S3Addressing(resolvedEndpoint)
	form.endpointURL = resolvedEndpoint
	form.legacy = usesLegacyEndpoints(ctx, tfBin, workdir)

	// Provider blocks and remote-state data blocks require the provider schema,
	// which only exists after init. So init emits a backend-only override.
	includeProvider := !isInit
	var keys []string
	var remoteStates []remoteState
	if includeProvider {
		keys, err = discoverEndpointKeys(ctx, tfBin, workdir, resolvedEndpoint, region, account, backend, form.legacy, logger)
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
		remoteStates = parseRemoteStates(workdir, logger)
	}

	written, err := generateOverride(overrideOptions{
		workdir:         workdir,
		fileName:        overrideFileName(),
		endpointURL:     resolvedEndpoint,
		region:          region,
		account:         account,
		endpointKeys:    keys,
		includeProvider: includeProvider,
		backend:         backend,
		remoteStates:    remoteStates,
		legacy:          form.legacy,
		logger:          logger,
	})
	if err != nil {
		return err
	}

	if dryRun() {
		// Leave the override in place for inspection and do not run terraform or
		// provision resources.
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

	if backend != nil {
		if err := provisionBackend(ctx, backend, form, logger); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			if errors.Is(err, awscli.ErrNotInstalled) {
				sink.Emit(output.ErrorEvent{
					Title:   "aws CLI not found in PATH",
					Summary: "lstk uses the AWS CLI to provision the S3 state bucket and lock table in LocalStack.",
					Actions: []output.ErrorAction{{Label: "Install AWS CLI:", Value: awscli.InstallURL}},
				})
				return output.NewSilentError(err)
			}
			return err
		}
	}

	return runTerraform(ctx, span, tfBin, args)
}

// discoverEndpointKeys probes the installed provider schema for the AWS endpoint
// keys via `providers schema -json`. When an S3 backend is declared the probe
// validates the backend config against what `init` cached in .terraform, so the
// LocalStack backend redirection must be present — otherwise terraform aborts
// with "Backend configuration block has changed" before emitting the schema. For
// that case we write a temporary backend-only override for the duration of the
// probe and remove it afterward, so the full override (which needs these keys for
// its provider blocks) is still generated exactly once by the caller.
func discoverEndpointKeys(ctx context.Context, tfBin, workdir, endpointURL, region, account string, backend *s3Backend, legacy bool, logger log.Logger) ([]string, error) {
	if backend == nil {
		return EndpointKeys(ctx, tfBin, workdir)
	}
	written, err := generateOverride(overrideOptions{
		workdir:     workdir,
		fileName:    overrideFileName(),
		endpointURL: endpointURL,
		region:      region,
		account:     account,
		backend:     backend,
		legacy:      legacy,
		logger:      logger,
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, p := range written {
			if rerr := os.Remove(p); rerr != nil && !os.IsNotExist(rerr) {
				logger.Error("terraform: failed to remove probe override %s: %v", p, rerr)
			}
		}
	}()
	return EndpointKeys(ctx, tfBin, workdir)
}

// ResolveChdir resolves terraform's -chdir=DIR value into an absolute-ish
// working directory: an absolute DIR is used as-is, a relative DIR is joined to
// the process working directory (matching how terraform interprets it).
func ResolveChdir(getwd, chdir string) string {
	if filepath.IsAbs(chdir) {
		return chdir
	}
	return filepath.Join(getwd, chdir)
}

// runTerraform executes terraform with stdio wired through to the user and
// propagates the exit code. A non-zero exit is wrapped as a silent error so
// lstk does not print an additional error line over terraform's own output.
func runTerraform(ctx context.Context, span trace.Span, tfBin string, args []string) error {
	cmd := exec.CommandContext(ctx, tfBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := proc.Run(cmd); err != nil {
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
