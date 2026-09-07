package config

import (
	"fmt"
	"strings"
)

// UpdateCheckMode is the value of the `[cli] update_check` config key (and of
// the LSTK_UPDATE_CHECK environment variable), governing the automatic update
// check on the start path only — an explicit `lstk update` always runs.
//
// UpdateCheckUnset is the zero value and means "no preference expressed", which
// is what lets a caller distinguish an unset key from an explicit "prompt" and
// fall through to the next source in the resolution order.
type UpdateCheckMode string

const (
	UpdateCheckUnset UpdateCheckMode = ""
	// UpdateCheckPrompt checks and, on an interactive start, blocks on a choice.
	UpdateCheckPrompt UpdateCheckMode = "prompt"
	// UpdateCheckNotify checks and emits a single non-blocking note.
	UpdateCheckNotify UpdateCheckMode = "notify"
	// UpdateCheckOff performs no check at all: no request, no output.
	UpdateCheckOff UpdateCheckMode = "off"
)

// updateCheckModes is the accepted set, in the order used to build error text.
var updateCheckModes = []UpdateCheckMode{UpdateCheckPrompt, UpdateCheckNotify, UpdateCheckOff}

// ParseUpdateCheckMode validates a raw update_check value. An empty string is
// valid and yields UpdateCheckUnset; anything else must match a mode exactly.
// Matching is deliberately strict — no trimming or case folding — so a typo
// surfaces as an error the user can see rather than being silently coerced into
// a mode they did not ask for.
func ParseUpdateCheckMode(s string) (UpdateCheckMode, error) {
	if s == "" {
		return UpdateCheckUnset, nil
	}
	for _, m := range updateCheckModes {
		if string(m) == s {
			return m, nil
		}
	}
	valid := make([]string, len(updateCheckModes))
	for i, m := range updateCheckModes {
		valid[i] = string(m)
	}
	return "", fmt.Errorf("invalid update_check value %q (must be one of: %s)", s, strings.Join(valid, ", "))
}
