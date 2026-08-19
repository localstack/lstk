package update

import (
	"fmt"
	"strings"
)

// CheckMode is the policy for the automatic update check lstk performs on the
// start path. It deliberately does not affect an explicit `lstk update`, which
// is a direct user action and always checks.
type CheckMode string

const (
	// CheckModePrompt asks whether to update, blocking on the answer. It is the
	// default, and only reachable on an interactive terminal.
	CheckModePrompt CheckMode = "prompt"
	// CheckModeNotify prints a one-line note and continues without waiting for
	// input. It is the default for installs owned by another package manager.
	CheckModeNotify CheckMode = "notify"
	// CheckModeOff performs no check at all — no network request, no output.
	CheckModeOff CheckMode = "off"
)

// CheckModes lists every valid mode in the order they appear in user-facing
// messages, mirroring config.SelectableEmulatorTypes.
var CheckModes = []CheckMode{CheckModePrompt, CheckModeNotify, CheckModeOff}

// ParseCheckMode validates a raw update_check value from config or the
// LSTK_UPDATE_CHECK environment variable.
//
// Unlike config.ParseEmulatorType this trims and lowercases first: the value is
// typed by hand into a shell or a CI env file, and the set of modes is closed
// and unambiguous, so "OFF" is unmistakably "off". An empty string is invalid —
// callers treat "unset" as "this source has no opinion" before calling here.
func ParseCheckMode(s string) (CheckMode, error) {
	normalized := CheckMode(strings.ToLower(strings.TrimSpace(s)))
	for _, mode := range CheckModes {
		if normalized == mode {
			return mode, nil
		}
	}

	valid := make([]string, 0, len(CheckModes))
	for _, mode := range CheckModes {
		valid = append(valid, string(mode))
	}
	return "", fmt.Errorf("invalid update_check value %q (must be one of: %s)", s, strings.Join(valid, ", "))
}
