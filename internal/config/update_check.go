package config

import (
	"fmt"
	"strconv"
)

// CheckForUpdateOnStartupDefault applies when neither the `[cli]
// check_for_update_on_startup` key nor LSTK_CHECK_FOR_UPDATE_ON_STARTUP is set.
const CheckForUpdateOnStartupDefault = true

// checkForUpdateOnStartupKey is the key's name within the [cli] table, shared
// by the reader, the writer and the pre-unmarshal validation.
const checkForUpdateOnStartupKey = "check_for_update_on_startup"

// ParseCheckForUpdateOnStartup parses the environment variable's raw value.
// Anything strconv.ParseBool rejects is reported rather than coerced, so a typo
// cannot silently disable update checks.
func ParseCheckForUpdateOnStartup(s string) (bool, error) {
	enabled, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid check_for_update_on_startup value %q (must be true or false)", s)
	}
	return enabled, nil
}
