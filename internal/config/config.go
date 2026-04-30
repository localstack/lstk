package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
			"port": DefaultAWSPort,
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

// Init loads the config file, searching the standard paths. If no config file
// exists, it creates one from the default template and returns firstRun=true.
func Init() (firstRun bool, err error) {
	viper.Reset()
	setDefaults()
	viper.SetConfigName(configName)
	viper.SetConfigType(configType)

	dirs, err := configSearchDirs()
	if err != nil {
		return false, err
	}
	for _, dir := range dirs {
		viper.AddConfigPath(dir)
	}

	if err := viper.ReadInConfig(); err != nil {
		var notFoundErr viper.ConfigFileNotFoundError
		if !errors.As(err, &notFoundErr) {
			if used := viper.ConfigFileUsed(); filepath.Ext(used) == ".yaml" || filepath.Ext(used) == ".yml" {
				return false, fmt.Errorf("%s is from an old lstk version; lstk now uses TOML format — remove it or replace it with a config.toml file", used)
			}
			return false, fmt.Errorf("failed to read config file: %w", err)
		}

		// No config found anywhere, create one using creation policy.
		creationDir, err := configCreationDir()
		if err != nil {
			return false, err
		}

		if err := os.MkdirAll(creationDir, 0755); err != nil {
			return false, fmt.Errorf("failed to create config directory: %w", err)
		}

		configPath := filepath.Join(creationDir, configFileName)
		f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return false, loadConfig(configPath)
			}
			return false, fmt.Errorf("failed to create config file: %w", err)
		}
		_, writeErr := f.WriteString(defaultConfigTemplate)
		closeErr := f.Close()
		if writeErr != nil {
			_ = os.Remove(configPath)
			return false, fmt.Errorf("failed to write config file: %w", writeErr)
		}
		if closeErr != nil {
			_ = os.Remove(configPath)
			return false, fmt.Errorf("failed to close config file: %w", closeErr)
		}

		return true, loadConfig(configPath)
	}
	return false, nil
}

func resolvedConfigPath() string {
	return viper.ConfigFileUsed()
}

func Set(key string, value any) error {
	viper.Set(key, value)
	return setInFile(viper.ConfigFileUsed(), key, value)
}

// setInFile inserts or updates a single "section.field" key in the TOML config
// file without rewriting unrelated content, preserving comments and formatting.
func setInFile(path, key string, value any) error {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return viper.WriteConfig()
	}
	section, field := parts[0], parts[1]

	// Encode value using go-toml for correct scalar quoting.
	type wrapper struct {
		V any `toml:"v"`
	}
	enc, err := toml.Marshal(wrapper{V: value})
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}
	line := strings.TrimSpace(string(enc))
	assignment := field + " =" + line[strings.IndexByte(line, '=')+1:]

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	content := string(data)

	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(field) + `\s*=.*$`)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, assignment)
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n[" + section + "]\n" + assignment + "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
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
