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

// ImageUpdatePolicy controls when lstk pulls container images during startup.
type ImageUpdatePolicy string

const (
	ImageUpdateAuto   ImageUpdatePolicy = "auto"   // pull only if missing or older than TTL
	ImageUpdateAlways ImageUpdatePolicy = "always"  // always pull
	ImageUpdateNever  ImageUpdatePolicy = "never"   // never pull automatically
)

type Config struct {
	Containers   []ContainerConfig            `mapstructure:"containers"`
	Env          map[string]map[string]string `mapstructure:"env"`
	UpdatePrompt bool                          `mapstructure:"update_prompt"`
	ImageUpdate  ImageUpdatePolicy             `mapstructure:"image_update"`
}

func setDefaults() {
	viper.SetDefault("containers", []map[string]any{
		{
			"type": "aws",
			"tag":  "latest",
			"port": "4566",
		},
	})
	viper.SetDefault("update_prompt", true)
	viper.SetDefault("image_update", string(ImageUpdateAuto))
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
	return viper.WriteConfig()
}

func DisableUpdatePrompt() error {
	return Set("update_prompt", false)
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
	switch cfg.ImageUpdate {
	case ImageUpdateAuto, ImageUpdateAlways, ImageUpdateNever:
	case "":
		cfg.ImageUpdate = ImageUpdateAuto
	default:
		return nil, fmt.Errorf("invalid image_update value %q (must be auto, always, or never)", cfg.ImageUpdate)
	}
	return &cfg, nil
}
