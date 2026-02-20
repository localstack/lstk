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

	dockerRegistry      = "localstack"
	localConfigFileName = "lstk.toml"
	userConfigFileName  = "config.toml"
)

var emulatorImages = map[EmulatorType]string{
	EmulatorAWS: "localstack-pro",
}

var emulatorHealthPaths = map[EmulatorType]string{
	EmulatorAWS: "/_localstack/health",
}

type Config struct {
	Containers []ContainerConfig `mapstructure:"containers"`
}

type ContainerConfig struct {
	Type EmulatorType `mapstructure:"type"`
	Tag  string       `mapstructure:"tag"`
	Port string       `mapstructure:"port"`
	Env  []string     `mapstructure:"env"`
}

func (c *ContainerConfig) Image() (string, error) {
	productName, err := c.ProductName()
	if err != nil {
		return "", err
	}
	tag := c.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s/%s:%s", dockerRegistry, productName, tag), nil
}

// Name returns the container name: "localstack-{type}" or "localstack-{type}-{tag}" if tag != latest
func (c *ContainerConfig) Name() string {
	tag := c.Tag
	if tag == "" || tag == "latest" {
		return fmt.Sprintf("localstack-%s", c.Type)
	}
	return fmt.Sprintf("localstack-%s-%s", c.Type, tag)
}

func (c *ContainerConfig) HealthPath() (string, error) {
	path, ok := emulatorHealthPaths[c.Type]
	if !ok {
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
	return path, nil
}

func (c *ContainerConfig) ProductName() (string, error) {
	productName, ok := emulatorImages[c.Type]
	if !ok {
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
	return productName, nil
}

func xdgConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "lstk"), nil
}

func osConfigDir() (string, error) {
	configHome, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	return filepath.Join(configHome, "lstk"), nil
}

func localConfigPath() string {
	return filepath.Join(".", localConfigFileName)
}

func configSearchPaths() ([]string, error) {
	xdgDir, err := xdgConfigDir()
	if err != nil {
		return nil, err
	}

	osDir, err := osConfigDir()
	if err != nil {
		return nil, err
	}

	return []string{
		// Priority order: project-local, then XDG-style home config, then OS-specific fallback.
		localConfigPath(),
		filepath.Join(xdgDir, userConfigFileName),
		filepath.Join(osDir, userConfigFileName),
	}, nil
}

func configCreationDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	homeConfigDir := filepath.Join(homeDir, ".config")
	// Creation policy differs from read fallback: prefer $HOME/.config only when it already exists.
	info, err := os.Stat(homeConfigDir)
	if err == nil {
		if info.IsDir() {
			return xdgConfigDir()
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to inspect %s: %w", homeConfigDir, err)
	}

	return osConfigDir()
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

func firstExistingConfigPath() (string, bool, error) {
	paths, err := configSearchPaths()
	if err != nil {
		return "", false, err
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("failed to inspect config path %s: %w", path, err)
		}
	}

	return "", false, nil
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

func ConfigDir() (string, error) {
	configPath, err := ConfigFilePath()
	if err != nil {
		return "", err
	}

	return filepath.Dir(configPath), nil
}

func resolvedConfigPath() string {
	return viper.ConfigFileUsed()
}

func ConfigFilePath() (string, error) {
	if resolved := resolvedConfigPath(); resolved != "" {
		// If Init already ran, use Viper's selected file directly.
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			return "", fmt.Errorf("failed to resolve absolute config path: %w", err)
		}
		return absResolved, nil
	}

	existingPath, found, err := firstExistingConfigPath()
	if err != nil {
		return "", err
	}
	if found {
		// Side-effect-free resolution for commands that skip Init (e.g. `lstk config path`).
		absPath, err := filepath.Abs(existingPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve absolute config path: %w", err)
		}
		return absPath, nil
	}

	creationDir, err := configCreationDir()
	if err != nil {
		return "", err
	}

	creationPath := filepath.Join(creationDir, userConfigFileName)
	absCreationPath, err := filepath.Abs(creationPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute config path: %w", err)
	}
	return absCreationPath, nil
}

func Get() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}
