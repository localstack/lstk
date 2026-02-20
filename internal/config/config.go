package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Containers []ContainerConfig `mapstructure:"containers"`
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
	viper.SetConfigFile(path)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	return nil
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
	viper.Reset()
	setDefaults()
	viper.SetConfigType("toml")
	viper.SetConfigFile(configPath)
	if err := viper.SafeWriteConfigAs(configPath); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
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
	return &cfg, nil
}
