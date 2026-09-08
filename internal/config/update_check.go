package config

import (
	"fmt"
	"strings"
)

// UpdateCheckMode is the `[cli] update_check` / LSTK_UPDATE_CHECK value. It
// governs only the automatic check on start; an explicit `lstk update` always
// runs. The zero value means "unset", which is what lets the command boundary
// fall through from the env var to the config key. The domain layer then treats
// unset and prompt identically, since install detection decides between
// prompting and a note either way.
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

// ParseUpdateCheckMode validates a raw update_check value; "" yields
// UpdateCheckUnset. Matching is exact — no trimming or case folding — so a typo
// is reported rather than coerced into a mode the user did not ask for.
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
