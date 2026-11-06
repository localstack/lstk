package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	envListRe = regexp.MustCompile(`(?m)^[ \t]*env[ \t]*=[ \t]*\[`)
	quotedRe  = regexp.MustCompile(`"[^"\n]*"|'[^'\n]*'`)
)

// SetEnvProfileVar sets key in [env.<profile>] and lists that profile last in
// the first [[containers]] block, so it wins. The file is restored if the value
// does not take effect.
func SetEnvProfileVar(profile, key, value string) (bool, error) {
	path := resolvedConfigPath()
	if path == "" {
		return false, errors.New("no config file to write to yet")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read config file: %w", err)
	}
	updated, err := addEnvReference(original, profile)
	if err != nil {
		return false, err
	}
	updated = setProfileVar(updated, profile, key, value)
	if bytes.Equal(updated, original) {
		return false, nil
	}
	if err := os.WriteFile(path, updated, 0644); err != nil {
		return false, fmt.Errorf("failed to write config file: %w", err)
	}
	if err := checkResolvedEnv(path, key, value); err != nil {
		if restoreErr := os.WriteFile(path, original, 0644); restoreErr != nil {
			return false, errors.Join(err, restoreErr)
		}
		return false, errors.Join(err, loadConfig(path))
	}
	return true, nil
}

func checkResolvedEnv(path, key, value string) error {
	if err := loadConfig(path); err != nil {
		return err
	}
	cfg, err := Get()
	if err != nil {
		return err
	}
	env, err := cfg.Containers[0].ResolvedEnv(cfg.Env)
	if err != nil {
		return err
	}
	got := ""
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == strings.ToUpper(key) {
			got = v
		}
	}
	if got != value {
		return fmt.Errorf("another env profile also sets %s and overrides this change", key)
	}
	return nil
}

func addEnvReference(data []byte, profile string) ([]byte, error) {
	header := containersHeaderRe.FindIndex(data)
	if header == nil {
		return nil, errors.New("no [[containers]] block found in config")
	}
	start, end := header[1], len(data)
	if next := tableHeaderRe.FindIndex(data[start:]); next != nil {
		end = start + next[0]
	}
	quoted := `"` + profile + `"`
	loc := envListRe.FindIndex(data[start:end])
	if loc == nil {
		return replace(data, start, start, "\nenv = ["+quoted+"]"), nil
	}
	open := start + loc[1]
	closeAt := bytes.IndexByte(data[open:end], ']')
	if closeAt < 0 {
		return nil, errors.New("could not parse the env list in the [[containers]] block")
	}
	names := quotedRe.FindAllIndex(data[open:open+closeAt], -1)
	for _, n := range names {
		if string(data[open+n[0]+1:open+n[1]-1]) == profile {
			return data, nil
		}
	}
	if len(names) == 0 {
		return replace(data, open, open, quoted), nil
	}
	at := open + names[len(names)-1][1]
	return replace(data, at, at, ", "+quoted), nil
}

func setProfileVar(data []byte, profile, key, value string) []byte {
	assignment := fmt.Sprintf("%s = %q", key, value)
	headerRe := regexp.MustCompile(`(?m)^[ \t]*\[env\.` + regexp.QuoteMeta(profile) + `\][ \t]*$`)
	header := headerRe.FindIndex(data)
	if header == nil {
		trimmed := len(bytes.TrimRight(data, "\n"))
		return replace(data, trimmed, len(data), "\n\n[env."+profile+"]\n"+assignment+"\n")
	}
	start, end := header[1], len(data)
	if next := tableHeaderRe.FindIndex(data[start:]); next != nil {
		end = start + next[0]
	}
	// viper lowercases keys.
	keyRe := regexp.MustCompile(`(?mi)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*("[^"\n]*"|'[^'\n]*'|[^ \t\n#]+)`)
	if loc := keyRe.FindSubmatchIndex(data[start:end]); loc != nil {
		return replace(data, start+loc[2], start+loc[3], fmt.Sprintf("%q", value))
	}
	return replace(data, start, start, "\n"+assignment)
}

func replace(data []byte, from, to int, s string) []byte {
	out := make([]byte, 0, len(data)-(to-from)+len(s))
	out = append(out, data[:from]...)
	out = append(out, s...)
	return append(out, data[to:]...)
}
