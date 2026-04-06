package config

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

//go:embed default_config.toml
var defaultConfigTemplate string

type CLIConfig struct {
	UpdatePrompt         bool   `mapstructure:"update_prompt"`
	UpdateSkippedVersion string `mapstructure:"update_skipped_version"`
}

type Config struct {
	Containers []ContainerConfig            `mapstructure:"containers"`
	Env        map[string]map[string]string `mapstructure:"env"`
	CLI        CLIConfig                    `mapstructure:"cli"`
}

func setDefaults() {
	viper.SetDefault("containers", []map[string]any{
		{
			"type": "aws",
			"tag":  "latest",
			"port": "4566",
		},
	})
	viper.SetDefault("cli.update_prompt", true)
}

func loadConfig(path string) error {
	viper.Reset()
	setDefaults()
	viper.SetConfigType("toml")
	viper.SetConfigFile(path)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	return nil
}

func InitFromPath(path string) error {
	return loadConfig(path)
}

func Init() error {
	viper.Reset()
	setDefaults()
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)

	dirs, err := configSearchDirs()
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		viper.AddConfigPath(dir)
	}

	if err := viper.ReadInConfig(); err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundErr) {
			if used := viper.ConfigFileUsed(); filepath.Ext(used) == ".yaml" || filepath.Ext(used) == ".yml" {
				return fmt.Errorf("%s is from an old lstk version; lstk now uses TOML format — remove it or replace it with a config.toml file", used)
			}
			return fmt.Errorf("failed to read config file: %w", err)
		}

		// No config found anywhere, create one using creation policy.
		creationDir, err := configCreationDir()
		if err != nil {
			return err
		}

		if err := os.MkdirAll(creationDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		configPath := filepath.Join(creationDir, configFileName)
		f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return loadConfig(configPath)
			}
			return fmt.Errorf("failed to create config file: %w", err)
		}
		_, writeErr := f.WriteString(defaultConfigTemplate)
		closeErr := f.Close()
		if writeErr != nil {
			_ = os.Remove(configPath)
			return fmt.Errorf("failed to write config file: %w", writeErr)
		}
		if closeErr != nil {
			_ = os.Remove(configPath)
			return fmt.Errorf("failed to close config file: %w", closeErr)
		}

		return loadConfig(configPath)
	}
	return nil
}

func resolvedConfigPath() string {
	return viper.ConfigFileUsed()
}

func Set(key string, value any) error {
	viper.Set(key, value)
	return setInFile(viper.ConfigFileUsed(), key, value)
}

// setInFile updates a single key in the TOML config file without
// rewriting unrelated keys (avoids Viper dumping all defaults).
func setInFile(path, key string, value any) error {
	// Split "cli.update_skipped_version" into section "cli" and field "update_skipped_version".
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		// Top-level keys: fall back to full rewrite.
		return viper.WriteConfig()
	}
	section, field := parts[0], parts[1]

	formatted := formatTOMLValue(value)
	targetLine := field + " = " + formatted

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var result []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inSection := false
	replaced := false
	sectionHeader := "[" + section + "]"

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Detect section headers.
		if strings.HasPrefix(trimmed, "[") {
			if trimmed == sectionHeader {
				inSection = true
			} else if inSection {
				// Leaving our section without having replaced — insert before the new section.
				if !replaced {
					result = append(result, targetLine)
					replaced = true
				}
				inSection = false
			}
		}

		// Replace existing key in the target section.
		if inSection && strings.HasPrefix(trimmed, field+" ") || inSection && strings.HasPrefix(trimmed, field+"=") {
			result = append(result, targetLine)
			replaced = true
			continue
		}

		result = append(result, line)
	}

	// Section exists but key was not found — append to end of file (still in section).
	if !replaced && inSection {
		result = append(result, targetLine)
		replaced = true
	}

	// Section doesn't exist at all — append section and key.
	if !replaced {
		if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
			result = append(result, "")
		}
		result = append(result, sectionHeader)
		result = append(result, targetLine)
	}

	output := strings.Join(result, "\n")
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	return os.WriteFile(path, []byte(output), 0644)
}

func formatTOMLValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

func DisableUpdatePrompt() error {
	return Set("cli.update_prompt", false)
}

func SetUpdateSkippedVersion(version string) error {
	return Set("cli.update_skipped_version", version)
}

func GetUpdateSkippedVersion() string {
	return viper.GetString("cli.update_skipped_version")
}

func Get() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if !viper.InConfig("cli.update_prompt") && viper.InConfig("update_prompt") {
		cfg.CLI.UpdatePrompt = viper.GetBool("update_prompt")
	}
	for i := range cfg.Containers {
		if err := cfg.Containers[i].Validate(); err != nil {
			return nil, fmt.Errorf("invalid container config: %w", err)
		}
	}
	return &cfg, nil
}
