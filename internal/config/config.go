package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type EmulatorType string

const (
	EmulatorAWS       EmulatorType = "aws"
	EmulatorSnowflake EmulatorType = "snowflake"
	EmulatorAzure     EmulatorType = "azure"
)

var emulatorImages = map[EmulatorType]string{
	EmulatorAWS: "localstack/localstack-pro",
}

type Config struct {
	Containers []ContainerConfig `mapstructure:"containers"`
}

type ContainerConfig struct {
	Type       EmulatorType `mapstructure:"type"`
	Tag        string       `mapstructure:"tag"`
	Port       string       `mapstructure:"port"`
	HealthPath string       `mapstructure:"health_path"`
	Env        []string     `mapstructure:"env"`
}

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

// Name returns the container name: "localstack-{type}" or "localstack-{type}-{tag}" if tag != latest
func (c *ContainerConfig) Name() string {
	tag := c.Tag
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("localstack-%s", c.Type)
	}
	return fmt.Sprintf("localstack-%s-%s", c.Type, tag)
}

func configDir() (string, error) {
	configHome, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	return filepath.Join(configHome, "lstk"), nil
}

func ConfigDir() (string, error) {
	return configDir()
}

func Init() error {
	dir, err := configDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(dir)

	viper.SetDefault("containers", []map[string]any{
		{
			"type":        "aws",
			"tag":         "latest",
			"port":        "4566",
			"health_path": "/_localstack/health",
		},
	})

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			if err := viper.SafeWriteConfig(); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}
			return nil
		}

		return fmt.Errorf("failed to read config file: %w", err)
	}

	return nil
}

func Get() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func ConfigFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}
