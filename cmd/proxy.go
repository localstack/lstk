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
