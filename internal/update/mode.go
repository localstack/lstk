package update

import "fmt"

// Update-check modes for LSTK_UPDATE_CHECK / config.toml's cli.update_check.
const (
	ModePrompt = "prompt" // ask interactively and wait
	ModeNotify = "notify" // print a one-line notice, don't wait
	ModeOff    = "off"    // skips the check entirely
)

// ValidateMode reports whether mode is one of the modes above. Empty string
// is not valid here; callers treat that as "unset" themselves.
func ValidateMode(mode string) error {
	switch mode {
	case ModePrompt, ModeNotify, ModeOff:
		return nil
	default:
		return fmt.Errorf("unknown update check mode %q: use prompt, notify, or off", mode)
	}
}
