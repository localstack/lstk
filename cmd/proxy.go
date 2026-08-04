package cmd

import (
	"os"
	"strconv"
	"strings"
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
