package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	localConfigDir = ".lstk"
	configName     = "config"
	configType     = "toml"
	configFileName = configName + "." + configType
)

// LogDir returns the standard path for diagnostic logs.
func LogDir() (string, error) {
	return logDirForOS(runtime.GOOS)
}

func logDirForOS(goos string) (string, error) {
	if goos == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(dir, "lstk"), nil
	}

	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "lstk"), nil
}

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

// FriendlyConfigPath returns a human-readable config path: relative for
// project-local configs, ~-prefixed for paths under $HOME, absolute otherwise.
func FriendlyConfigPath() (string, error) {
	absPath, err := ConfigFilePath()
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		resolved = absPath
	}

	cwd, err := os.Getwd()
	if err == nil {
		if rel, relErr := filepath.Rel(cwd, resolved); relErr == nil && isInside(rel) {
			return rel, nil
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		resolvedHome, evalErr := filepath.EvalSymlinks(home)
		if evalErr != nil {
			resolvedHome = home
		}
		if rel, relErr := filepath.Rel(resolvedHome, resolved); relErr == nil && isInside(rel) {
			return filepath.Join("~", rel), nil
		}
	}

	return absPath, nil
}

// isInside reports whether a relative path produced by filepath.Rel stays
// inside the base directory (i.e. does not climb out via "..").
func isInside(rel string) bool {
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
