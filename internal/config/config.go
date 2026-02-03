package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// EmulatorType represents a supported emulator type.
type EmulatorType string

const (
	EmulatorAWS       EmulatorType = "aws"
	EmulatorSnowflake EmulatorType = "snowflake"
	EmulatorAzure     EmulatorType = "azure"
)

// emulatorImages maps emulator types to their Docker images.
var emulatorImages = map[EmulatorType]string{
	EmulatorAWS: "localstack/localstack-pro",
}

// Config holds the application configuration.
type Config struct {
	Containers []ContainerConfig `mapstructure:"containers"`
}

// ContainerConfig holds the configuration for a single container.
type ContainerConfig struct {
	Type       EmulatorType `mapstructure:"type"`
	Tag        string       `mapstructure:"tag"`
	Port       string       `mapstructure:"port"`
	HealthPath string       `mapstructure:"health_path"`
	Env        []string     `mapstructure:"env"`
}

// Image returns the full Docker image reference for this container.
func (c *ContainerConfig) Image() (string, error) {
	baseImage, ok := emulatorImages[c.Type]
	if !ok {
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
	tag := c.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", baseImage, tag), nil
}

// Name returns the generated container name based on type and tag.
func (c *ContainerConfig) Name() string {
	tag := c.Tag
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("localstack-%s", c.Type)
	}
	return fmt.Sprintf("localstack-%s-%s", c.Type, tag)
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
			"type":        "aws",
			"tag":         "latest",
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
