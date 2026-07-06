package extension

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// The runtime-context contract is conveyed to an extension through exactly two
// environment variables:
//
//   - EnvAPIVersion (LSTK_EXT_API_VERSION) — a flat integer kept outside the JSON
//     payload so an extension can check contract compatibility before parsing.
//   - EnvContext (LSTK_EXT_CONTEXT) — a single JSON object (see Context) carrying
//     the resolved config directory, auth token, non-interactive state, and the
//     list of running emulators.
//
// APIVersion is bumped ONLY on a breaking change (a field removed or
// repurposed); adding a field does not bump it. Extensions therefore detect
// additive fields by their presence in the JSON object — not via the version —
// and use the version only to refuse a contract generation they predate. So any
// field added after version 1 must be distinguishable when absent (omitempty /
// pointer / null) for that presence check to work.
const (
	EnvAPIVersion = "LSTK_EXT_API_VERSION"
	EnvContext    = "LSTK_EXT_CONTEXT"
)

// envPrefix is the common prefix of every contract variable. Inherited values
// under this prefix are stripped from the environment before the resolved
// contract is applied, so lstk fully owns the LSTK_EXT_ namespace handed to the
// child: every LSTK_EXT_* an extension sees came from this lstk invocation. This
// is an ownership invariant, not a correctness fix for the two vars we always
// set — exec.Cmd deduplicates Env keeping the last entry, so those override an
// inherited value regardless; the strip's job is to also remove LSTK_EXT_* names
// lstk does not set.
const envPrefix = "LSTK_EXT_"

// Emulator describes one running LocalStack emulator in the context payload.
type Emulator struct {
	Type     string `json:"type"`     // emulator type, e.g. "aws", "snowflake", "azure"
	Endpoint string `json:"endpoint"` // full URL, e.g. "http://localhost:4566"
	Port     string `json:"port"`     // resolved host port, e.g. "4566"
}

// Context is the resolved runtime context lstk conveys to an extension, rendered
// as the LSTK_EXT_CONTEXT JSON object. The command boundary populates it
// (resolving running emulators, config dir, auth token, interactivity, and the
// resolved --json flag) and Environ renders it. An empty AuthToken is omitted
// from the JSON; Emulators is always present, marshalling to [] when no
// emulator is running so an extension always decodes a list.
type Context struct {
	ConfigDir      string     `json:"configDir"`
	AuthToken      string     `json:"authToken,omitempty"`
	NonInteractive bool       `json:"nonInteractive"`
	JSON           bool       `json:"json"`
	Emulators      []Emulator `json:"emulators"`
}

// Environ layers the resolved contract on top of the inherited host environment
// base (typically os.Environ()), returning a new slice suitable for
// exec.Cmd.Env. The host environment is preserved so extensions inherit the
// user's PATH, locale, and tool configuration; only LSTK_EXT_API_VERSION and
// LSTK_EXT_CONTEXT are added. Any inherited LSTK_EXT_* is stripped first so a
// stray value cannot shadow lstk's resolved context.
func (c Context) Environ(base []string) ([]string, error) {
	if c.Emulators == nil {
		c.Emulators = []Emulator{}
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal extension context: %w", err)
	}

	env := make([]string, 0, len(base)+2)
	for _, entry := range base {
		if strings.HasPrefix(entry, envPrefix) {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, EnvAPIVersion+"="+strconv.Itoa(APIVersion))
	env = append(env, EnvContext+"="+string(payload))
	return env, nil
}
