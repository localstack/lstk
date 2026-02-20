package config

import (
	"fmt"
	"os"
	"path/filepath"
)

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
