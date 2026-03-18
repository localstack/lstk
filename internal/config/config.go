package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

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

const defaultConfigTemplate = `# lstk configuration file
# Run 'lstk config path' to see where this file lives.

# Each [[containers]] block defines an emulator instance.
# You can define multiple to run them side by side.
[[containers]]
type = "aws"     # Emulator type. Currently supported: "aws"
tag  = "latest"  # Docker image tag, e.g. "latest", "3.8.0", "latest-arm64"
port = "4566"    # Host port the emulator will be accessible on
# env = []       # Named environment profiles to apply (see [env.*] sections below)

# Environment profiles let you group environment variables and reference
# them by name in one or more containers via the 'env' field above.
#
# Example variables based on commonly used current config options:
#
#   DEBUG=1          - Enable verbose logging
#   PERSISTENCE=1    - Persist LocalStack state across restarts
#   ENFORCE_IAM=1    - Enable IAM enforcement
#
# See full list of configuration options:
# > https://docs.localstack.cloud/references/configuration/
#
# Example:
#
# [env.debug]
# DEBUG = "1"
#
# [env.persist]
# PERSISTENCE = "1"
#
# [env.security]
# ENFORCE_IAM = "1"
`

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
	if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0644); err != nil {
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
	for i := range cfg.Containers {
		if err := cfg.Containers[i].Validate(); err != nil {
			return nil, fmt.Errorf("invalid container config: %w", err)
		}
	}
	return &cfg, nil
}
