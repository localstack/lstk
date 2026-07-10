package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// typeLineRe matches a `type = "value"` assignment anchored at the start of a
// line (leading indentation allowed). Anchoring means commented lines (which
// start with '#') and unrelated keys such as `content_type = "..."` never match,
// and only the value is captured (group 1) so a rewrite can splice it in place
// without disturbing quotes, spacing, or trailing comments.
var typeLineRe = regexp.MustCompile(`(?m)^[ \t]*type[ \t]*=[ \t]*["'](\w+)["']`)

// ParseEmulatorType validates a raw emulator type string against the selectable
// types and returns the corresponding EmulatorType.
func ParseEmulatorType(s string) (EmulatorType, error) {
	for _, t := range SelectableEmulatorTypes {
		if string(t) == s {
			return t, nil
		}
	}
	valid := make([]string, len(SelectableEmulatorTypes))
	for i, t := range SelectableEmulatorTypes {
		valid[i] = string(t)
	}
	return "", fmt.Errorf("invalid emulator type %q (must be one of: %s)", s, strings.Join(valid, ", "))
}

// SetEmulatorType rewrites the emulator type in the config file and reloads.
// No-op if the requested type is already set.
func SetEmulatorType(to EmulatorType) error {
	path := resolvedConfigPath()
	if path == "" {
		return fmt.Errorf("no config file loaded")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	// Only the first match — the active [[containers]] block's type — is
	// rewritten, and only the captured value is replaced. This leaves any other
	// type-like keys, commented-out example blocks, and the original formatting
	// untouched.
	loc := typeLineRe.FindSubmatchIndex(data)
	if loc == nil {
		return fmt.Errorf("no emulator type field found in config")
	}
	valueStart, valueEnd := loc[2], loc[3]
	if EmulatorType(data[valueStart:valueEnd]) == to {
		return nil
	}
	updated := make([]byte, 0, len(data)-(valueEnd-valueStart)+len(to))
	updated = append(updated, data[:valueStart]...)
	updated = append(updated, string(to)...)
	updated = append(updated, data[valueEnd:]...)
	if err := os.WriteFile(path, updated, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return loadConfig(path)
}
