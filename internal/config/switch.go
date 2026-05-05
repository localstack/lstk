package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const awsContainerBlock = `[[containers]]
type = "aws"
tag  = "latest"
port = "4566"
# volume = ""    # Host directory for persistent state (default: OS cache dir)
# env = []       # Named environment profiles to apply (see [env.*] sections below)`

const snowflakeContainerBlock = `[[containers]]
type = "snowflake"
tag  = "latest"
port = "4566"
# volume = ""    # Host directory for persistent state (default: OS cache dir)
# env = []       # Named environment profiles to apply (see [env.*] sections below)`

// SwitchEmulator updates the config file to activate the given emulator type.
// Active container blocks for other types are commented out. If a previously
// commented block for the target type exists it is restored; otherwise a fresh
// block is appended. No-op when the target is already the only active emulator.
func SwitchEmulator(to EmulatorType) error {
	path := resolvedConfigPath()
	if path == "" {
		return fmt.Errorf("no config file loaded")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	updated, changed := switchEmulatorContent(string(data), to)
	if !changed {
		return nil
	}

	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return loadConfig(path)
}

func switchEmulatorContent(content string, to EmulatorType) (updated string, changed bool) {
	lines := strings.Split(content, "\n")
	blocks := parseContainerBlocks(lines)

	if isEmulatorAlreadyActive(blocks, to) {
		return content, false
	}

	newLines := make([]string, len(lines))
	copy(newLines, lines)

	hasActiveTarget := false
	restoredCommented := false

	for _, b := range blocks {
		switch {
		case !b.isCommented && b.emulType == to:
			hasActiveTarget = true
		case !b.isCommented && b.emulType != to:
			for i := b.start; i < b.end; i++ {
				if newLines[i] != "" {
					newLines[i] = "# " + newLines[i]
				}
			}
		case b.isCommented && b.emulType == to && !restoredCommented:
			for i := b.start; i < b.end; i++ {
				newLines[i] = strings.TrimPrefix(newLines[i], "# ")
			}
			restoredCommented = true
		}
	}

	result := strings.Join(newLines, "\n")
	if !hasActiveTarget && !restoredCommented {
		tmpl := containerBlockTemplate(to)
		result = strings.TrimRight(result, "\n") + "\n\n" + tmpl + "\n"
	}

	return result, true
}

func isEmulatorAlreadyActive(blocks []containerBlock, to EmulatorType) bool {
	hasActiveTarget := false
	for _, b := range blocks {
		if b.isCommented {
			continue
		}
		if b.emulType != to {
			return false
		}
		hasActiveTarget = true
	}
	return hasActiveTarget
}

type containerBlock struct {
	start       int
	end         int // exclusive
	emulType    EmulatorType
	isCommented bool
}

func parseContainerBlocks(lines []string) []containerBlock {
	var blocks []containerBlock
	n := len(lines)

	for i := 0; i < n; i++ {
		trimmed := strings.TrimSpace(lines[i])
		isActive := trimmed == "[[containers]]"
		isCommented := trimmed == "# [[containers]]"
		if !isActive && !isCommented {
			continue
		}

		end := n
		for j := i + 1; j < n; j++ {
			t := strings.TrimSpace(lines[j])
			if t == "[[containers]]" || t == "# [[containers]]" {
				end = j
				break
			}
			if len(t) > 0 && t[0] == '[' {
				end = j
				break
			}
		}

		blocks = append(blocks, containerBlock{
			start:       i,
			end:         end,
			emulType:    detectBlockType(lines[i:end], isCommented),
			isCommented: isCommented,
		})
		i = end - 1
	}
	return blocks
}

var typeLineRe = regexp.MustCompile(`type\s*=\s*"(\w+)"`)

func detectBlockType(lines []string, isCommented bool) EmulatorType {
	for _, line := range lines {
		effective := strings.TrimSpace(line)
		if isCommented {
			effective = strings.TrimSpace(strings.TrimPrefix(effective, "#"))
		}
		if m := typeLineRe.FindStringSubmatch(effective); m != nil {
			return EmulatorType(strings.ToLower(m[1]))
		}
	}
	return ""
}

func containerBlockTemplate(t EmulatorType) string {
	switch t {
	case EmulatorAWS:
		return awsContainerBlock
	case EmulatorSnowflake:
		return snowflakeContainerBlock
	default:
		return ""
	}
}
