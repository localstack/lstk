// Package cli implements the `lstk terraform` proxy. It wraps the user's
// invocation of the terraform binary so that AWS resources land on LocalStack
// rather than real AWS, without requiring the user to edit their .tf files.
//
// End-to-end flow, orchestrated by Exec:
//
//  1. Resolve options. The caller (cmd/terraform.go) hands us a user-supplied
//     Options plus the raw terraform argv. resolveOptionsWithDefaults() applies
//     default values if not already specified.
//
//  2. Locate the terraform binary on PATH. Done up front so we surface a
//     clear error before touching the user's filesystem.
//
//  3. Bootstrap the state backend (optional). If the user configured an S3
//     backend, terraform's `init` expects the bucket (and DynamoDB lock
//     table, when configured) to already exist. We create them on LocalStack
//     via the `aws` CLI before terraform runs. Failures here are non-fatal —
//     we emit a warning and let terraform surface a clearer error of its own.
//
//  4. Run terraform. stdin/stdout/stderr stream through so the user sees
//     native progress output; the AWS SDK env vars in Options.Endpoints are
//     injected (preserving any the user already set so customer-supplied
//     AWS_ENDPOINT_URL_* values always win); Ctrl+C is forwarded.
package cli

import (
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

	"github.com/localstack/lstk/internal/output"
)

// Endpoint maps to one AWS SDK endpoint env var. Service "" → AWS_ENDPOINT_URL
// (the default); Service "S3" → AWS_ENDPOINT_URL_S3; Service "DYNAMODB" →
// AWS_ENDPOINT_URL_DYNAMODB; and so on, matching the SDK's <SERVICE>
// suffix convention (uppercase, underscores).
type Endpoint struct {
	Service string
	URL     string
}

// Options that can be provided by the user. resolveOptionsWithDefaults
// supplies defaults for unset fields; after that single pass the value is
// immutable from the perspective of every other helper in the package.
type Options struct {
	// Endpoints are the AWS endpoint defaults to inject into terraform's
	// environment. Each is applied with setIfAbsent semantics so a customer
	// value already in os.Environ() always wins.
	Endpoints []Endpoint

	// WorkingDir is the directory Terraform runs in (defaults to the current
	// working directory). Honors -chdir=DIR on the args.
	WorkingDir string

	// TerraformBin overrides the terraform executable name (TF_CMD).
	TerraformBin string

	// AccessKey / Region map to the AWS SDK env vars AWS_ACCESS_KEY_ID and
	// AWS_DEFAULT_REGION.
	AccessKey string
	Region    string

	// CustomizeAccessKey rewrites a leading "A" of AccessKey to "L" so a real
	// AWS access key cannot accidentally hit LocalStack. Mirrors tflocal's
	// CUSTOMIZE_ACCESS_KEY behavior.
	CustomizeAccessKey bool

	// AutoCreateBackendResources, when true, creates the S3 state bucket and
	// the DynamoDB lock table on LocalStack before invoking terraform.
	AutoCreateBackendResources bool
}

// endpointFor returns the URL configured for the given AWS SDK service
// suffix. Exact match wins; otherwise the default endpoint (Service == "")
// is returned. Returns "" when neither is present.
func (r Options) endpointFor(service string) string {
	var fallback string
	for _, ep := range r.Endpoints {
		switch ep.Service {
		case service:
			return ep.URL
		case "":
			fallback = ep.URL
		}
	}
	return fallback
}

// resolveOptionsWithDefaults inserts default values for options the caller
// did not provide and bakes one-time transformations (e.g. the
// CustomizeAccessKey rewrite) into the value. It is called exactly once per
// Exec invocation; no other helper in this package applies defaults, which
// keeps the rest of the pipeline free of `if x == "" { x = default }` clutter.
//
// The args parameter is the user's terraform argv; `-chdir=DIR` if present
// wins over opts.WorkingDir. Variadic so test callers that don't care about
// args can omit it.
func resolveOptionsWithDefaults(opts Options, args ...string) Options {
	if cd := extractChdir(args); cd != "" {
		opts.WorkingDir = cd
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}
	if opts.TerraformBin == "" {
		opts.TerraformBin = "terraform"
	}
	if opts.AccessKey == "" {
		opts.AccessKey = "test"
	}
	if opts.CustomizeAccessKey {
		// deactivateAccessKey is idempotent ("Lxxx" stays "Lxxx"), so we
		// leave the flag set — buildEnv uses it as the signal to force-replace
		// the env var even when the parent shell already had one set.
		// Without that force-replace, an accidentally-real AWS_ACCESS_KEY_ID
		// in the parent env would bypass the deactivation safety net.
		opts.AccessKey = deactivateAccessKey(opts.AccessKey)
	}
	if opts.Region == "" {
		opts.Region = "us-east-1"
	}
	return opts
}

// deactivateAccessKey rewrites the leading "A" of an AWS access key to "L" so
// live credentials cannot accidentally hit LocalStack. Returns the input
// unchanged when it does not start with "A".
func deactivateAccessKey(key string) string {
	if key == "" {
		return key
	}
	if key[0] == 'A' {
		return "L" + key[1:]
	}
	return key
}

// Exec is the package entry point. See the file-level documentation for the
// algorithm.
//
// stdin/stdout/stderr are wired directly to the terraform subprocess so the
// user sees its native progress output. The args slice is the raw terraform
// argv (everything after `lstk terraform`).
func Exec(ctx context.Context, opts Options, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/iac/terraform/cli").Start(ctx, "terraform cli")
	defer span.End()

	options := resolveOptionsWithDefaults(opts, args...)

	tfBin, err := exec.LookPath(options.TerraformBin)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("terraform CLI not found in PATH — install it from https://developer.hashicorp.com/terraform/downloads")
	}

	span.SetAttributes(
		attribute.StringSlice("terraform.args", args),
		attribute.String("terraform.endpoint_url", options.endpointFor("")),
		attribute.String("terraform.s3_endpoint_url", options.endpointFor("S3")),
	)

	if options.AutoCreateBackendResources {
		backend, err := parseS3Backend(options.WorkingDir)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "lstk: warning: could not parse .tf files for backend bootstrap: %v\n", err)
		} else if backend != nil {
			if err := ensureBackendResources(ctx, options, *backend, stderr); err != nil {
				_, _ = fmt.Fprintf(stderr, "lstk: warning: could not auto-create backend resources: %v\n", err)
			}
		}
	}

	cmd := exec.CommandContext(ctx, tfBin, args...)
	cmd.Env = buildEnv(os.Environ(), options)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Forward Ctrl+C to terraform rather than killing the wrapper. Without
	// this, ctx cancellation would SIGKILL terraform and leave its state
	// half-written; SIGINT lets terraform unwind cleanly.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Signal(os.Interrupt)
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Terraform printed its own error already; wrap as SilentError so
			// the cmd-level handler doesn't double-print "Error: ...".
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

// extractChdir scans the terraform argv for `-chdir=DIR` and returns DIR.
// Empty if no such flag is present. We need this because terraform changes
// its working directory before reading any files.
func extractChdir(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-chdir=") {
			return strings.TrimPrefix(a, "-chdir=")
		}
	}
	return ""
}

// buildEnv prepares the environment passed to the terraform child process.
//
// AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_DEFAULT_REGION follow
// setIfAbsent semantics so anything the user pre-set in the parent shell
// survives — except when CustomizeAccessKey is on, where the safety-net
// force-replaces the access key.
//
// AWS_ENDPOINT_URL[_*] env vars are handled differently: options.Endpoints is
// the canonical, deduplicated list assembled at the cmd boundary (it already
// resolves user-supplied vs lstk-default precedence). We therefore strip any
// AWS_ENDPOINT_URL[_*] entries from the inherited base env before appending
// options.Endpoints, ensuring exactly one entry per service in the child env.
func buildEnv(base []string, options Options) []string {
	env := make([]string, 0, len(base)+3+len(options.Endpoints))
	for _, e := range base {
		if isEndpointEnv(e) {
			continue
		}
		env = append(env, e)
	}
	if options.CustomizeAccessKey {
		setOrReplace(&env, "AWS_ACCESS_KEY_ID", options.AccessKey)
	} else {
		setIfAbsent(&env, "AWS_ACCESS_KEY_ID", options.AccessKey)
	}
	setIfAbsent(&env, "AWS_SECRET_ACCESS_KEY", "test")
	setIfAbsent(&env, "AWS_DEFAULT_REGION", options.Region)
	for _, ep := range options.Endpoints {
		if ep.URL == "" {
			continue
		}
		key := "AWS_ENDPOINT_URL"
		if ep.Service != "" {
			key = "AWS_ENDPOINT_URL_" + ep.Service
		}
		env = append(env, key+"="+ep.URL)
	}
	return env
}

// isEndpointEnv reports whether an env entry (KEY=VALUE) is an AWS endpoint
// URL — the bare AWS_ENDPOINT_URL or any AWS_ENDPOINT_URL_<SERVICE>.
func isEndpointEnv(e string) bool {
	return strings.HasPrefix(e, "AWS_ENDPOINT_URL=") || strings.HasPrefix(e, "AWS_ENDPOINT_URL_")
}

// setIfAbsent appends key=value to env unless an entry with that key prefix
// already exists. Linear scan is fine — env slices are small (a few dozen
// entries) and this runs once per Exec call.
func setIfAbsent(env *[]string, key, value string) {
	prefix := key + "="
	for _, e := range *env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	*env = append(*env, prefix+value)
}

// setOrReplace ensures env contains exactly key=value, overwriting any prior
// entry. Used for CUSTOMIZE_ACCESS_KEY: an accidentally-real AWS key in the
// parent env must not survive into the terraform child.
func setOrReplace(env *[]string, key, value string) {
	prefix := key + "="
	for i, e := range *env {
		if strings.HasPrefix(e, prefix) {
			(*env)[i] = prefix + value
			return
		}
	}
	*env = append(*env, prefix+value)
}
