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

// containersHeaderRe matches the `[[containers]]` array-of-tables header, and
// tableHeaderRe matches any TOML table header. Anchoring at line start skips
// commented-out (`#`-prefixed) headers. Together they bound the type search to
// the active block so a `type` key in any other table is never rewritten.
var (
	containersHeaderRe = regexp.MustCompile(`(?m)^[ \t]*\[\[containers\]\]`)
	tableHeaderRe      = regexp.MustCompile(`(?m)^[ \t]*\[`)
)

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
	// The type key belongs to the active [[containers]] block, so scope the
	// search to that block — from just after its header to the next table header
	// (or EOF). This guarantees a `type` key in any other table (e.g. an [env.*]
	// profile) is never mistaken for the emulator type. Only the captured value
	// is replaced, leaving commented-out example blocks and the original
	// formatting untouched.
	header := containersHeaderRe.FindIndex(data)
	if header == nil {
		return fmt.Errorf("no [[containers]] block found in config")
	}
	blockStart := header[1]
	block := data[blockStart:]
	if next := tableHeaderRe.FindIndex(block); next != nil {
		block = block[:next[0]]
	}
	loc := typeLineRe.FindSubmatchIndex(block)
	if loc == nil {
		return fmt.Errorf("no emulator type field found in config")
	}
	valueStart, valueEnd := blockStart+loc[2], blockStart+loc[3]
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
