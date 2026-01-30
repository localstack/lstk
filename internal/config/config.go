package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the application configuration.
type Config struct {
	Containers []ContainerConfig `mapstructure:"containers"`
}

// ContainerConfig holds the configuration for a single container.
type ContainerConfig struct {
	Image      string   `mapstructure:"image"`
	Name       string   `mapstructure:"name"`
	Port       string   `mapstructure:"port"`
	HealthPath string   `mapstructure:"health_path"`
	Env        []string `mapstructure:"env"`
}

// configDir returns the lstk configuration directory.
func configDir() (string, error) {
	configHome, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	return filepath.Join(configHome, "lstk"), nil
}

// ConfigDir returns the lstk configuration directory path.
func ConfigDir() (string, error) {
	return configDir()
}

// Init initializes Viper with the configuration file and defaults.
func Init() error {
	dir, err := configDir()
	if err != nil {
		return err
	}

	// Ensure config directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(dir)

	viper.SetDefault("containers", []map[string]any{
		{
			"image":       "localstack/localstack-pro:latest",
			"name":        "localstack-aws",
			"port":        "4566",
			"health_path": "/_localstack/health",
		},
	})

	// Read config file if it exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}

	return nil
}

// Get returns the current configuration.
func Get() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

// ConfigFilePath returns the path to the config file.
func ConfigFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}
