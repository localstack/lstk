package cmd

import (
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
