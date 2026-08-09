package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/validate"
)

type globalFlags struct {
	nonInteractive bool
	configPath     string
}

// stripGlobalFlags removes lstk's persistent flags (--non-interactive and
// --config) from a proxy command's arguments, returning the remaining args and
// the extracted values. Proxy commands set DisableFlagParsing, so without this
// these flags would be forwarded to the wrapped binary (which rejects them as
// unknown) and their effect silently lost. Both --flag value and --flag=value
// forms are recognized, in any position.
//
// --config is stripped only in its long form: it has no -c shorthand, and a
// short -c must pass through untouched because wrapped tools claim it (CDK's
// -c/--context, SAM's -c/--cached), so stripping it would break those commands.
//
// --json is deliberately NOT recognized here: unlike --non-interactive/--config,
// which configure lstk's own wrapping mechanics, --json is purely about lstk's
// own output rendering, which proxy commands don't have — their output is the
// wrapped tool's own. Some wrapped tools already define their own --json/-json
// (e.g. Terraform's `-json` flag on plan/apply/show), so lstk must not shadow
// it; --json falls through untouched via the default case below.
func stripGlobalFlags(args []string) ([]string, globalFlags) {
	out := make([]string, 0, len(args))
	var gf globalFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--non-interactive":
			gf.nonInteractive = true
		case strings.HasPrefix(arg, "--non-interactive="):
			// A malformed value still strips the flag (it must never reach the
			// wrapped binary) and enables the mode, matching the user's intent.
			v, err := strconv.ParseBool(strings.TrimPrefix(arg, "--non-interactive="))
			gf.nonInteractive = err != nil || v
		case arg == "--config":
			if i+1 < len(args) {
				gf.configPath = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--config="):
			gf.configPath = strings.TrimPrefix(arg, "--config=")
		default:
			out = append(out, arg)
		}
	}
	return out, gf
}

// proxySubcommand returns the safe leading command-path tokens of a proxy
// command's raw args for telemetry, e.g. "s3 ls" for `lstk aws s3 ls
// s3://bucket`. Only leading non-flag tokens are collected: collection stops at
// the first flag-like arg so a flag's value can never be mistaken for a
// subcommand. The token limit follows each CLI's grammar so a positional value
// is not recorded for flat commands such as `cdk deploy MyStack` or `terraform
// import ADDRESS ID`; each recorded token is capped at 64 runes.
//
// Known limitation: a flag preceding the subcommand tokens (e.g. `aws
// --profile foo s3 ls`) stops collection immediately, recording an empty
// subcommand even though "s3 ls" follows. This is accepted as the safer
// tradeoff over risking a flag or its value being misrecorded as a
// subcommand token.
func proxySubcommand(command string, args []string) string {
	args, _ = stripGlobalFlags(args)
	limit := 1
	if len(args) > 0 {
		limit = proxySubcommandTokenLimit(command, args[0])
	}
	tokens := make([]string, 0, limit)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		if r := []rune(arg); len(r) > 64 {
			arg = string(r[:64])
		}
		tokens = append(tokens, arg)
		if len(tokens) == limit {
			break
		}
	}
	return strings.Join(tokens, " ")
}

func proxySubcommandTokenLimit(command, firstToken string) int {
	switch command {
	case "aws":
		// AWS reserves its first two positions for the service and operation;
		// user-supplied values come later.
		return 2
	case "terraform":
		switch firstToken {
		case "metadata", "providers", "state", "workspace":
			return 2
		}
	case "sam":
		switch firstToken {
		case "local", "pipeline", "remote":
			return 2
		}
	}
	return 1
}

// jsonPrecedesCommandName reports whether --json (or --json=<value>) appears in
// the raw command line before the literal token calledAs — the resolved proxy
// command's own name/alias (e.g. "aws", "terraform"/"tf", "az"). Proxy commands
// set DisableFlagParsing, so Cobra's own command resolution (Command.Find /
// argsMinusFirstX) strips only the matched command token from the argument
// list and never runs flag parsing at all for that command — regardless of
// where --json appeared relative to it, it ends up in the same args slice
// stripGlobalFlags scans. Recovering the position therefore requires scanning
// raw os.Args directly, mirroring rejectPreSubcommandFlags's technique
// (cmd/iac.go) one window higher: before the command name, rather than between
// it and the wrapped tool's own action.
//
// The result uses the same boolean-aware parsing stripGlobalFlags applies to
// --non-interactive: a bare --json or --json=true resolves true; --json=false
// resolves false (an explicit opt-out, not a rejection); a malformed value
// resolves true, matching the user's evident intent to enable it.
func jsonPrecedesCommandName(calledAs string) bool {
	cmdIdx := -1
	for i, a := range os.Args {
		if a == calledAs {
			cmdIdx = i
			break
		}
	}
	if cmdIdx <= 0 {
		return false
	}
	result := false
	for _, a := range os.Args[1:cmdIdx] {
		switch {
		case a == "--json":
			result = true
		case strings.HasPrefix(a, "--json="):
			v, err := strconv.ParseBool(strings.TrimPrefix(a, "--json="))
			result = err != nil || v
		}
	}
	return result
}

// stripPreCommandEndpointURL returns the value of a --endpoint-url (or
// --endpoint-url=<value>) that precedes the literal token calledAs in the raw
// command line, along with a corrected args slice (equivalent to what Cobra
// hands PreRunE/RunE for a DisableFlagParsing command) with that occurrence
// removed. An occurrence AFTER calledAs is left untouched in the returned
// args — for `aws` specifically, this preserves the aws CLI's own native
// --endpoint-url flag (a post-command one must reach the wrapped binary
// unchanged); for the other four proxy commands there's no such collision,
// but the same pre-command-only rule applies for consistency, mirroring
// --json's existing treatment.
//
// This must actually remove the pre-command occurrence, not just detect it
// (unlike jsonPrecedesCommandName, which leaves --json in place since it's a
// bare boolean signal): Cobra's own command resolution strips only the
// matched command token for a DisableFlagParsing command, regardless of
// where a flag appeared relative to it — so an unstripped pre-command
// --endpoint-url would leak into the forwarded args, duplicating alongside
// lstk's own injected --endpoint-url built from the resolved target.
func stripPreCommandEndpointURL(calledAs string) (args []string, value string, found bool) {
	cmdIdx := -1
	for i, a := range os.Args {
		if a == calledAs {
			cmdIdx = i
			break
		}
	}
	if cmdIdx <= 0 {
		return os.Args[1:], "", false
	}

	before := os.Args[1:cmdIdx]
	after := os.Args[cmdIdx+1:]

	stripped := make([]string, 0, len(before))
	for i := 0; i < len(before); i++ {
		a := before[i]
		switch {
		case a == "--endpoint-url":
			if i+1 < len(before) {
				value, found = before[i+1], true
				i++
				continue
			}
			stripped = append(stripped, a)
		case strings.HasPrefix(a, "--endpoint-url="):
			value, found = strings.TrimPrefix(a, "--endpoint-url="), true
		default:
			stripped = append(stripped, a)
		}
	}
	return append(stripped, after...), value, found
}

// leadingFlags selects which lstk-specific flags a proxy command recognizes in
// leading position — between the command's own name and the wrapped tool's
// action.
type leadingFlags struct {
	// account recognizes --account. All four proxies set it; cdk parses the
	// flag only to reject it at the command boundary with an explanation,
	// which still requires it to be consumed rather than forwarded.
	account bool
	// region recognizes --region. The three IaC proxies set it because their
	// wrapped tools define no equivalent flag. `lstk aws` leaves it false: the
	// AWS CLI has its own global --region, which must reach it untouched in
	// every position. Claiming it there would also be actively harmful — lstk
	// would have to re-encode it as AWS_DEFAULT_REGION, and an environment
	// region outranks a localstack profile's own `region`, silently overriding
	// it (see execEnv in internal/awscli).
	region bool
	// chdir reads terraform's global -chdir=DIR without consuming it.
	chdir bool
}

// leadingFlagSamples supplies an example value per lstk-specific leading flag.
// Used only to build the placement error in rejectPreSubcommandFlags.
var leadingFlagSamples = map[string]string{
	"--region":  "us-west-2",
	"--account": "111111111111",
}

// rejectPreSubcommandFlags returns an error if any of flagNames appears before
// the subcommand token calledAs:
//
//	lstk --account 222222222222 sam deploy   rejected
//	lstk sam --account 222222222222 deploy   the supported placement
//
// The recognized set is per-command (see leadingFlags): a pre-command --region is
// rejected for terraform but forwarded to the AWS CLI for aws, which claims only
// --account.
func rejectPreSubcommandFlags(calledAs string, flagNames ...string) error {
	cmdIdx := -1
	for i, a := range os.Args {
		if a == calledAs {
			cmdIdx = i
			break
		}
	}
	if cmdIdx <= 0 {
		return nil
	}
	for _, a := range os.Args[1:cmdIdx] {
		for _, name := range flagNames {
			if a == name || strings.HasPrefix(a, name+"=") {
				return fmt.Errorf("%s must appear after the %s subcommand (e.g. `lstk %s %s %s ...`)",
					strings.Join(flagNames, " and "), calledAs, calledAs, flagNames[0], leadingFlagSamples[flagNames[0]])
			}
		}
	}
	return nil
}

// stripLeadingProxyFlags extracts the lstk flags selected by opts from the
// leading run — everything before the wrapped tool's action — and forwards every
// other token unchanged. Both --flag value and --flag=value forms are accepted;
// a leading lstk flag missing its value is an error.
//
// The run ends at the action, not at the first token lstk does not own, so
// lstk's flags work in any order among the tool's own. Stopping early instead
// leaked --account to the tool, which rejects it as an unknown option, for an
// ordering distinction no user could see.
//
// Locating the action without knowing every tool's flags rests on one
// assumption: a bare token after a flag that may still take a value (no "=") is
// that value. At most one is absorbed per flag, so scanning always halts by the
// second consecutive bare token. That bound is what protects a genuine
// --account belonging to the tool: the AWS CLI defines one on ten operations,
// and every one follows a service and an operation — two bare tokens — so
// scanning has stopped before lstk could reach it.
//
// -chdir is read for lstk's working-directory resolution but kept in the args,
// since terraform must also see it to switch directories; its "=" marks it
// self-contained, so the action after it is not mistaken for its value.
func stripLeadingProxyFlags(args []string, opts leadingFlags) (remaining []string, region, account, chdir string, err error) {
	i := 0
	// Set when the previous forwarded token was a wrapped-tool flag that may
	// still consume a value, so the next bare token belongs to it rather than
	// being the action.
	pendingValue := false
	for i < len(args) {
		arg := args[i]
		switch {
		case opts.region && arg == "--region":
			if i+1 >= len(args) {
				return nil, "", "", "", fmt.Errorf("--region requires a value")
			}
			region = args[i+1]
			i += 2
			pendingValue = false
		case opts.region && strings.HasPrefix(arg, "--region="):
			region = strings.TrimPrefix(arg, "--region=")
			i++
			pendingValue = false
		case opts.account && arg == "--account":
			if i+1 >= len(args) {
				return nil, "", "", "", fmt.Errorf("--account requires a value")
			}
			account = args[i+1]
			i += 2
			pendingValue = false
		case opts.account && strings.HasPrefix(arg, "--account="):
			account = strings.TrimPrefix(arg, "--account=")
			i++
			pendingValue = false
		case strings.HasPrefix(arg, "-"):
			// A flag belonging to the wrapped tool: forward it and keep looking
			// for lstk's flags on the far side of it.
			if opts.chdir && strings.HasPrefix(arg, "-chdir=") {
				chdir = strings.TrimPrefix(arg, "-chdir=")
			}
			remaining = append(remaining, arg)
			pendingValue = !strings.Contains(arg, "=")
			i++
		case pendingValue:
			remaining = append(remaining, arg)
			pendingValue = false
			i++
		default:
			return append(remaining, args[i:]...), region, account, chdir, nil
		}
	}
	return remaining, region, account, chdir, nil
}

// resolveAccountSelection applies the precedence --account flag →
// AWS_ACCESS_KEY_ID → test, and reports whether the caller explicitly selected a
// LocalStack account.
//
// A selection is a validated --account, or an ambient AWS_ACCESS_KEY_ID that is
// itself a 12-digit account id — the documented way to address a specific
// LocalStack account. An ambient value of any other shape is deliberately not a
// selection, so a stray real credential in a developer's shell cannot displace a
// configured profile as the credentials source (see execEnv in internal/awscli).
//
// A flag value must be exactly 12 digits. An AWS_ACCESS_KEY_ID value is not
// validated, but is run through DeactivateAccessKey so a real key (AKIA…/ASIA…)
// accidentally present in the environment is never written into a generated
// override or sent to LocalStack; the validated 12-digit flag is used as-is
// (it cannot begin with "A").
func resolveAccountSelection(flag string) (account string, selected bool, err error) {
	if flag != "" {
		if err := validate.AWSAccountID(flag); err != nil {
			return "", false, fmt.Errorf("--account must be a 12-digit AWS account id, got %q", flag)
		}
		return flag, true, nil
	}
	if v := os.Getenv("AWS_ACCESS_KEY_ID"); v != "" {
		return awsconfig.DeactivateAccessKey(v), validate.AWSAccountID(v) == nil, nil
	}
	return "test", false, nil
}

// resolveAccount is resolveAccountSelection for the callers that always encode
// the resolved account and do not care how it was chosen (terraform, cdk, sam).
func resolveAccount(flag string) (string, error) {
	account, _, err := resolveAccountSelection(flag)
	return account, err
}
