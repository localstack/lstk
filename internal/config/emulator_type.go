package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var typeLineRe = regexp.MustCompile(`type\s*=\s*["'](\w+)["']`)

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
	m := typeLineRe.FindStringSubmatch(string(data))
	if m == nil {
		return fmt.Errorf("no emulator type field found in config")
	}
	if EmulatorType(m[1]) == to {
		return nil
	}
	updated := typeLineRe.ReplaceAllString(string(data), `type = "`+string(to)+`"`)
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return loadConfig(path)
}
