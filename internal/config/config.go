package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

//go:embed default_config.toml
var defaultConfigTemplate string

type Config struct {
	Containers []ContainerConfig            `mapstructure:"containers"`
	Env        map[string]map[string]string `mapstructure:"env"`
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
	// Reuse the same ordered path resolution used by ConfigFilePath.
	existingPath, found, err := firstExistingConfigPath()
	if err != nil {
		return err
	}
	if found {
		return loadConfig(existingPath)
	}

	// No config found anywhere, create one using creation policy.
	creationDir, err := configCreationDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(creationDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(creationDir, userConfigFileName)
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

func resolvedConfigPath() string {
	return viper.ConfigFileUsed()
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
