package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// envListRe matches the opening of an `env = [` assignment at line start, so a
// commented-out `# env = []` never matches. quotedRe matches one TOML string
// in that list.
var (
	envListRe = regexp.MustCompile(`(?m)^[ \t]*env[ \t]*=[ \t]*\[`)
	quotedRe  = regexp.MustCompile(`"[^"\n]*"|'[^'\n]*'`)
)

// SetEnvProfileVar makes the first [[containers]] block apply the [env.<profile>]
// profile with key set to value. The profile is appended to the block's env
// list, so it is applied after (and wins over) the profiles already there, and
// the table is created at the end of the file when missing.
//
// The file is edited in place so comments and formatting survive. The edit is
// rolled back if the result does not load, or if a profile listed after this
// one still overrides the value.
func SetEnvProfileVar(profile, key, value string) error {
	path := resolvedConfigPath()
	if path == "" {
		return errors.New("no config file to write to yet")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	updated, err := withEnvReference(original, profile)
	if err != nil {
		return err
	}
	updated = withProfileVar(updated, profile, key, value)
	if bytes.Equal(updated, original) {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	if err := os.WriteFile(path, updated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := checkEnvProfileVar(path, profile, key, value); err != nil {
		if restoreErr := os.WriteFile(path, original, info.Mode().Perm()); restoreErr != nil {
			return fmt.Errorf("%w (restoring %s also failed: %v)", err, path, restoreErr)
		}
		return errors.Join(err, loadConfig(path))
	}
	return nil
}

// checkEnvProfileVar reloads the config and confirms the first container now
// resolves key to value.
func checkEnvProfileVar(path, profile, key, value string) error {
	if err := loadConfig(path); err != nil {
		return err
	}
	cfg, err := Get()
	if err != nil {
		return err
	}
	if len(cfg.Containers) == 0 {
		return errors.New("no [[containers]] block found in config")
	}
	winner := ""
	for _, name := range cfg.Containers[0].Env {
		for k := range cfg.Env[name] {
			if strings.EqualFold(k, key) {
				winner = name
			}
		}
	}
	if winner != profile {
		return fmt.Errorf("[env.%s] is applied after [env.%s] and also sets %s; remove %s from [env.%s] or move %q to the end of the env list", winner, profile, key, key, winner, profile)
	}
	if got := cfg.Env[profile][strings.ToLower(key)]; got != value {
		return fmt.Errorf("could not set %s in [env.%s]; edit %s by hand", key, profile, path)
	}
	return nil
}

// firstContainersBlock returns the byte range of the first [[containers]]
// block's body: from just after its header to the next table header (or EOF).
func firstContainersBlock(data []byte) (start, end int, ok bool) {
	header := containersHeaderRe.FindIndex(data)
	if header == nil {
		return 0, 0, false
	}
	start, end = header[1], len(data)
	if next := tableHeaderRe.FindIndex(data[start:]); next != nil {
		end = start + next[0]
	}
	return start, end, true
}

// withEnvReference adds profile to the end of the first [[containers]] block's
// env list, adding an env line after the type line when the block has none.
func withEnvReference(data []byte, profile string) ([]byte, error) {
	start, end, ok := firstContainersBlock(data)
	if !ok {
		return nil, errors.New("no [[containers]] block found in config")
	}
	block := data[start:end]
	quoted := `"` + profile + `"`

	if loc := envListRe.FindIndex(block); loc != nil {
		open := loc[1]
		closeOffset := bytes.IndexByte(block[open:], ']')
		if closeOffset < 0 {
			return nil, errors.New("could not parse the env list in the [[containers]] block")
		}
		inner := block[open : open+closeOffset]
		names := quotedRe.FindAllIndex(inner, -1)
		for _, n := range names {
			if string(inner[n[0]+1:n[1]-1]) == profile {
				return data, nil
			}
		}
		switch {
		case len(names) > 0:
			return splice(data, start+open+names[len(names)-1][1], ", "+quoted), nil
		case len(bytes.TrimSpace(inner)) == 0:
			return replace(data, start+open, start+open+closeOffset, quoted), nil
		default:
			return splice(data, start+open, quoted+","), nil
		}
	}

	at, indent := start, ""
	if loc := typeLineRe.FindIndex(block); loc != nil {
		at = start + loc[1]
		line := block[loc[0]:loc[1]]
		indent = string(line[:len(line)-len(bytes.TrimLeft(line, " \t"))])
	}
	nl := bytes.IndexByte(data[at:], '\n')
	if nl < 0 {
		return append(append([]byte{}, data...), "\n"+indent+"env = ["+quoted+"]\n"...), nil
	}
	return splice(data, at+nl+1, indent+"env = ["+quoted+"]\n"), nil
}

// withProfileVar sets key to value in the [env.<profile>] table, creating the
// table at the end of the file when it does not exist.
func withProfileVar(data []byte, profile, key, value string) []byte {
	assignment := key + " = " + fmt.Sprintf("%q", value)
	q := regexp.QuoteMeta(profile)
	headerRe := regexp.MustCompile(`(?m)^[ \t]*\[env\.(?:` + q + `|"` + q + `")\][ \t]*(?:#.*)?$`)
	header := headerRe.FindIndex(data)
	if header == nil {
		return append(bytes.TrimRight(data, "\n"), "\n\n[env."+profile+"]\n"+assignment+"\n"...)
	}

	start, end := header[1], len(data)
	if next := tableHeaderRe.FindIndex(data[start:]); next != nil {
		end = start + next[0]
	}
	// Keys are matched case-insensitively because viper lowercases them.
	keyRe := regexp.MustCompile(`(?mi)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*("[^"\n]*"|'[^'\n]*'|[^ \t\n#]+)`)
	if loc := keyRe.FindSubmatchIndex(data[start:end]); loc != nil {
		return replace(data, start+loc[2], start+loc[3], fmt.Sprintf("%q", value))
	}
	return splice(data, start, "\n"+assignment)
}

func splice(data []byte, at int, s string) []byte {
	return replace(data, at, at, s)
}

func replace(data []byte, from, to int, s string) []byte {
	out := make([]byte, 0, len(data)-(to-from)+len(s))
	out = append(out, data[:from]...)
	out = append(out, s...)
	return append(out, data[to:]...)
}
