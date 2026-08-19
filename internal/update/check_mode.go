package update

import (
	"fmt"
	"strings"
)

// CheckMode is the policy for the automatic update check on the start path. It
// does not affect an explicit `lstk update`, which always checks.
type CheckMode string

const (
	CheckModePrompt CheckMode = "prompt" // ask, blocking on the answer (default)
	CheckModeNotify CheckMode = "notify" // one-line note, no waiting for input
	CheckModeOff    CheckMode = "off"    // no check: no request, no output
)

// ParseCheckMode validates a raw update_check value from config or
// LSTK_UPDATE_CHECK. Unlike config.ParseEmulatorType it trims and lowercases
// first: the value is hand-typed into a shell or CI env file, and three closed
// values leave no ambiguity. An empty string is invalid — callers treat "unset"
// as "no opinion" before calling here.
func ParseCheckMode(s string) (CheckMode, error) {
	switch mode := CheckMode(strings.ToLower(strings.TrimSpace(s))); mode {
	case CheckModePrompt, CheckModeNotify, CheckModeOff:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid update_check value %q (must be one of: %s, %s, %s)", s, CheckModePrompt, CheckModeNotify, CheckModeOff)
	}
}
