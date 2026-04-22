package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

//go:embed default_config.toml
var defaultConfigTemplate string

type CLIConfig struct {
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

// setInFile updates a single key in the TOML config file using go-toml/v2.
// The key must be in "section.field" form (e.g. "cli.update_skipped_version").
func setInFile(path, key string, value any) error {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return viper.WriteConfig()
	}
	section, field := parts[0], parts[1]

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if doc[section] == nil {
		doc[section] = map[string]any{}
	}
	sectionMap, ok := doc[section].(map[string]any)
	if !ok {
		return fmt.Errorf("config section %q is not a table", section)
	}
	sectionMap[field] = value
	doc[section] = sectionMap

	out, err := toml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, out, 0644)
}

func SetUpdateSkippedVersion(version string) error {
	return Set("cli.update_skipped_version", version)
}

func Get() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	for i := range cfg.Containers {
		if err := cfg.Containers[i].Validate(); err != nil {
			return nil, fmt.Errorf("invalid container config: %w", err)
		}
	}
	return &cfg, nil
}
