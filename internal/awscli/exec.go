package awscli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/proc"
)

const InstallURL = "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"

var ErrNotInstalled = errors.New("aws CLI not found in PATH")

func CheckInstalled() error {
	if _, err := exec.LookPath("aws"); err != nil {
		return ErrNotInstalled
	}
	return nil
}

// helpFlags are the flags/tokens the aws CLI recognizes as a help request.
// "help" is a bare pseudo-subcommand the aws CLI accepts at any command level
// (`aws help`, `aws s3 help`), equivalent to -h/--help.
var helpFlags = map[string]bool{"-h": true, "--help": true, "help": true}

// IsHelp reports whether args requests the aws CLI's help output. The aws CLI
// answers this without needing a running emulator or resolved endpoint.
func IsHelp(args []string) bool {
	for _, a := range args {
		if helpFlags[a] {
			return true
		}
	}
	return false
}

// ExecOptions configures a single `aws` invocation.
type ExecOptions struct {
	// EndpointURL is injected as --endpoint-url. Empty skips it (the help path).
	EndpointURL string
	// Account is the resolved LocalStack account, written to AWS_ACCESS_KEY_ID
	// wherever lstk supplies credentials. LocalStack derives the account from
	// the access key id it receives.
	Account string
	// UseProfile selects the localstack AWS profile. See execEnv for why this
	// happens through AWS_PROFILE rather than a --profile argument.
	UseProfile bool
	// AccountSelected reports that the user explicitly asked for Account (via
	// --account, or a 12-digit AWS_ACCESS_KEY_ID) rather than it being the
	// default. It only matters alongside UseProfile, where it decides whether
	// the environment or the profile supplies credentials.
	AccountSelected bool
	// UsePTY runs the child under a pseudo-terminal.
	UsePTY bool
}

// Exec runs `aws <args...>` against opts.EndpointURL. When opts.UsePTY is true
// (lstk's stdout and stderr are both terminals), the child's output goes through
// a pseudo-terminal merged into stdout — see proc.RunInPTY for why; otherwise
// stdout/stderr are wired as given.
func Exec(ctx context.Context, opts ExecOptions, stdout, stderr io.Writer, args []string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/awscli").Start(ctx, "aws cli")
	defer span.End()

	awsBin, err := exec.LookPath("aws")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ErrNotInstalled
	}

	cmdArgs := make([]string, 0, len(args)+2)
	if opts.EndpointURL != "" {
		cmdArgs = append(cmdArgs, "--endpoint-url", opts.EndpointURL)
	}
	cmdArgs = append(cmdArgs, args...)

	span.SetAttributes(
		attribute.StringSlice("aws.args", args),
		attribute.Bool("aws.use_profile", opts.UseProfile),
		attribute.Bool("aws.account_selected", opts.AccountSelected),
	)

	cmd := exec.CommandContext(ctx, awsBin, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Env = execEnv(os.Environ(), opts)

	var runErr error
	started := false
	if opts.UsePTY {
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
			span.SetAttributes(attribute.Int("aws.exit_code", exitErr.ExitCode()))
			span.SetStatus(codes.Error, "aws cli exited non-zero")
			return output.NewSilentError(err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// execEnv builds the child environment for the aws CLI. There are three
// credential sources, and which one applies decides what may be set:
//
//	no profile            → lstk supplies credentials and a default region
//	profile + account     → AWS_PROFILE, credentials from the environment
//	profile, no account   → AWS_PROFILE, credentials from the profile
//
// The profile is selected through AWS_PROFILE rather than a --profile argument
// because the two resolve credentials differently: botocore drops the
// environment credential provider from its chain when a profile arrives as an
// explicit instance variable, which --profile sets and AWS_PROFILE does not. The
// environment form is therefore what lets an account be expressed while the
// profile still supplies region, output, s3 addressing style, and everything
// else the user put in it. Selecting an account never means dropping the profile.
//
// While a profile is in use lstk seeds no defaults of its own: environment
// variables outrank config-file values, so seeding AWS_DEFAULT_REGION would
// silently override the profile's own region.
//
// PYTHONUNBUFFERED stops a pip-installed (non-frozen) aws CLI from
// block-buffering stdout when it gets a pipe instead of a terminal (DEVX-1026);
// the official frozen v2 binary ignores it — that build's buffering is what the
// PTY in Exec fixes. (Python treats any non-empty value as enabled, so a
// user-set value is left alone.)
func execEnv(base []string, opts ExecOptions) []string {
	var env []string
	if !opts.UseProfile {
		env = BuildEnv(base, opts.Account)
		setIfAbsent(&env, "PYTHONUNBUFFERED", "1")
		return env
	}

	env = make([]string, len(base), len(base)+4)
	copy(env, base)
	set(&env, "AWS_PROFILE", awsconfig.ProfileName)
	// AWS_PROFILE outranks AWS_DEFAULT_PROFILE in the botocore version measured
	// here, but the two are resolved from one ordered list whose order has
	// varied; removing the deprecated spelling makes the selection unambiguous
	// rather than version-dependent. The terraform and sam proxies strip it too.
	remove(&env, "AWS_DEFAULT_PROFILE")
	remove(&env, "AWS_SESSION_TOKEN")

	if opts.AccountSelected {
		// Both halves of the pair, always: EnvProvider fails the whole
		// invocation with "Partial credentials found in env, missing:
		// AWS_SECRET_ACCESS_KEY" if it finds an access key id without a secret,
		// and a --profile argument is no longer there to shield it.
		set(&env, "AWS_ACCESS_KEY_ID", opts.Account)
		setIfAbsent(&env, "AWS_SECRET_ACCESS_KEY", "test")
	} else {
		// Removed, not left alone: with the environment provider back in the
		// chain, ambient credentials would otherwise beat the profile — losing
		// an account a user pinned in the [localstack] credentials section, and
		// risking the partial pair above.
		remove(&env, "AWS_ACCESS_KEY_ID")
		remove(&env, "AWS_SECRET_ACCESS_KEY")
	}

	setIfAbsent(&env, "PYTHONUNBUFFERED", "1")
	return env
}

// BuildEnv seeds LocalStack-compatible credentials and region for a child aws
// CLI invocation that resolves no profile. A non-empty account overrides
// AWS_ACCESS_KEY_ID outright, since LocalStack derives the account from it; an
// empty account keeps the set-if-absent mock default. The secret and region are
// only defaults, so an explicit user value continues to win.
//
// An ambient AWS_SESSION_TOKEN is dropped either way: lstk supplies the
// credentials itself, a token from an unrelated session cannot correspond to
// them, and the endpoint is always LocalStack. This matches the stripping the
// terraform and sam proxies already perform.
func BuildEnv(base []string, account string) []string {
	env := make([]string, len(base), len(base)+3)
	copy(env, base)

	if account != "" {
		set(&env, "AWS_ACCESS_KEY_ID", account)
	} else {
		setIfAbsent(&env, "AWS_ACCESS_KEY_ID", "test")
	}
	setIfAbsent(&env, "AWS_SECRET_ACCESS_KEY", "test")
	setIfAbsent(&env, "AWS_DEFAULT_REGION", "us-east-1")
	remove(&env, "AWS_SESSION_TOKEN")

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

// set overrides key, removing any existing entries first so the child sees a
// single unambiguous value.
func set(env *[]string, key, value string) {
	remove(env, key)
	*env = append(*env, key+"="+value)
}

func remove(env *[]string, key string) {
	prefix := key + "="
	out := (*env)[:0]
	for _, e := range *env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	*env = out
}
