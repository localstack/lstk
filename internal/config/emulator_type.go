package config

import (
	"fmt"
	"os"
	"regexp"
)

var typeLineRe = regexp.MustCompile(`type\s*=\s*["'](\w+)["']`)

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
