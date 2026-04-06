package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	localConfigDir = ".lstk"
	configName     = "config"
	configType     = "toml"
	configFileName = configName + "." + configType
)

func ConfigFilePath() (string, error) {
	if resolved := resolvedConfigPath(); resolved != "" {
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

	creationPath := filepath.Join(creationDir, configFileName)
	absCreationPath, err := filepath.Abs(creationPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute config path: %w", err)
	}
	return absCreationPath, nil
}

func ExistingConfigFilePath() (string, bool, error) {
	existingPath, found, err := firstExistingConfigPath()
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}

	absPath, err := filepath.Abs(existingPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve absolute config path: %w", err)
	}
	return absPath, true, nil
}

func ConfigDir() (string, error) {
	configPath, err := ConfigFilePath()
	if err != nil {
		return "", err
	}

	return filepath.Dir(configPath), nil
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

// configSearchDirs returns directories to search for config.toml, in priority order:
// project-local (.lstk/), XDG-style home config, OS-specific fallback.
func configSearchDirs() ([]string, error) {
	xdgDir, err := xdgConfigDir()
	if err != nil {
		return nil, err
	}

	osDir, err := osConfigDir()
	if err != nil {
		return nil, err
	}

	return []string{
		filepath.Join(".", localConfigDir),
		xdgDir,
		osDir,
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

func firstExistingConfigPath() (string, bool, error) {
	dirs, err := configSearchDirs()
	if err != nil {
		return "", false, err
	}

	for _, dir := range dirs {
		path := filepath.Join(dir, configFileName)
		if _, err := os.Stat(path); err == nil {
			return path, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("failed to inspect config path %s: %w", path, err)
		}
	}

	return "", false, nil
}
