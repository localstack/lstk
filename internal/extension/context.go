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
// (resolving running emulators, config dir, auth token, interactivity, the
// resolved --json flag, and the telemetry session id) and Environ renders it. An
// empty AuthToken is omitted from the JSON; Emulators is always present,
// marshalling to [] when no emulator is running so an extension always decodes a
// list.
//
// SessionID is lstk's per-process telemetry correlation id, conveyed so an
// extension emitting its own telemetry can join it to the ext:<name> event lstk
// records for the same invocation. It is omitted when lstk telemetry is disabled
// (there is no session to correlate), which means absence is ambiguous to the
// extension: it cannot tell a telemetry-disabled lstk from an lstk predating the
// field. Like every field added after version 1 it is detected by presence, not
// by LSTK_EXT_API_VERSION.
//
// MachineID is the anonymized machine identity lstk stamps on its own telemetry
// events — already the salted hash, never a raw Docker or system id — conveyed so
// an extension emitting telemetry reports the same machine without re-deriving
// it. Conveying it exposes nothing new: it is the prepared hash, and the child
// environment already carries AuthToken. It is omitted when lstk telemetry is
// disabled, which a disabled client makes absent together with SessionID (it
// computes neither), so absence is ambiguous in the same way SessionID's is.
//
// EndpointURL is the endpoint lstk was asked to target instead of a locally
// managed emulator, resolved with lstk's own source precedence (--endpoint-url,
// then LSTK_ENDPOINT_URL, then AWS_ENDPOINT_URL) and conveyed verbatim: it is
// not validated, normalized, or probed. A malformed or unreachable value reaches
// the extension unchanged on purpose — judging it is the extension's job, and an
// extension diagnosing a broken endpoint has to see exactly what the user set.
// It is omitted when no endpoint source was given, i.e. the invocation targets
// the default local emulator. Note that Emulators is independent of it: that
// array is what local Docker discovery found, not what lstk was told to target.
type Context struct {
	ConfigDir      string     `json:"configDir"`
	AuthToken      string     `json:"authToken,omitempty"`
	NonInteractive bool       `json:"nonInteractive"`
	JSON           bool       `json:"json"`
	SessionID      string     `json:"sessionId,omitempty"`
	MachineID      string     `json:"machineId,omitempty"`
	EndpointURL    string     `json:"endpointUrl,omitempty"`
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
