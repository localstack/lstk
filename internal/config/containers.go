package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type EmulatorType string

const (
	EmulatorAWS       EmulatorType = "aws"
	EmulatorSnowflake EmulatorType = "snowflake"
	EmulatorAzure     EmulatorType = "azure"

	DefaultAWSPort = "4566"
	dockerRegistry = "localstack"
)

var emulatorDisplayNames = map[EmulatorType]string{
	EmulatorAWS:       "AWS",
	EmulatorSnowflake: "Snowflake",
	EmulatorAzure:     "Azure",
}

// SelectableEmulatorTypes lists the emulator types available for interactive selection,
// in the order they should be presented. The selection key for each type is its first character.
var SelectableEmulatorTypes = []EmulatorType{EmulatorAWS, EmulatorSnowflake}

func (e EmulatorType) SelectionKey() string {
	return string(e)[0:1]
}

func (e EmulatorType) ShortName() string {
	if name, ok := emulatorDisplayNames[e]; ok {
		return name
	}
	return string(e)
}

func (e EmulatorType) DisplayName() string {
	return fmt.Sprintf("LocalStack %s Emulator", e.ShortName())
}
var emulatorHealthPaths = map[EmulatorType]string{
	EmulatorAWS:       "/_localstack/health",
	EmulatorSnowflake: "/_localstack/health",
}

var knownImages = []struct {
	Type        EmulatorType
	ProductName string
	Default     bool
}{
	{EmulatorAWS, "localstack-pro", true},
	{EmulatorAWS, "localstack", false},
	{EmulatorSnowflake, "snowflake", true},
}

func EmulatorTypeForImage(image string) EmulatorType {
	repo, _, _ := strings.Cut(image, ":")
	for _, e := range knownImages {
		if dockerRegistry+"/"+e.ProductName == repo {
			return e.Type
		}
	}
	return ""
}

func KnownImageRepos() []string {
	repos := make([]string, len(knownImages))
	for i, e := range knownImages {
		repos[i] = dockerRegistry + "/" + e.ProductName
	}
	return repos
}

func KnownImageReposForType(t EmulatorType) []string {
	var repos []string
	for _, e := range knownImages {
		if e.Type == t {
			repos = append(repos, dockerRegistry+"/"+e.ProductName)
		}
	}
	return repos
}


type ContainerConfig struct {
	Type   EmulatorType `mapstructure:"type"`
	Tag    string       `mapstructure:"tag"`
	Port   string       `mapstructure:"port"`
	Volume string       `mapstructure:"volume"`
	// Env is a list of named environment references defined in the top-level [env.*] config sections.
	Env []string `mapstructure:"env"`
}

// VolumeDir returns the host directory to mount into the container for persistence/caching.
// If Volume is set in the config, it is returned as-is. Otherwise, a default is computed
// from os.UserCacheDir()/lstk/volume/<container-name>.
func (c *ContainerConfig) VolumeDir() (string, error) {
	if c.Volume != "" {
		return c.Volume, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "lstk", "volume", c.Name()), nil
}

// zeroPaddedMonthTagRe matches calendar-versioned tags where the month is zero-padded
// (e.g. "2026.04", "2026.04.1-amd64"), which the license API does not accept.
var zeroPaddedMonthTagRe = regexp.MustCompile(`^(\d{4}\.)0([1-9].*)$`)

// validTagRe mirrors Docker's tag format rules: alphanumerics, dots, hyphens, underscores;
// must not start with a dot or hyphen; max 128 characters.
var validTagRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

// prevMonthExample returns the previous calendar month as a tag example, e.g. "2026.4".
func prevMonthExample() string {
	y, m, _ := time.Now().Date()
	m--
	if m == 0 {
		m, y = 12, y-1
	}
	return fmt.Sprintf("%d.%d", y, int(m))
}

func validateTag(tag string) error {
	if tag == "" {
		return nil
	}
	if len(tag) > 128 || !validTagRe.MatchString(tag) {
		return fmt.Errorf("tag %q is not supported — try a tag like %q or \"latest\" in your config file", tag, prevMonthExample())
	}
	m := zeroPaddedMonthTagRe.FindStringSubmatch(tag)
	if m == nil {
		return nil
	}
	suggested := m[1] + m[2]
	return fmt.Errorf("tag %q is not supported — try %q or \"latest\" in your config file", tag, suggested)
}

func (c *ContainerConfig) Validate() error {
	if err := validateTag(c.Tag); err != nil {
		return err
	}
	if c.Port == "" {
		return fmt.Errorf("port is required for %s emulator", c.Type)
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil {
		return fmt.Errorf("port %q is not a valid number", c.Port)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is out of range (must be 1–65535)", port)
	}
	return nil
}

// ResolvedEnv resolves the container's named environment references into KEY=value pairs.
// namedEnvs is the top-level [env.*] map from Config.
func (c *ContainerConfig) ResolvedEnv(namedEnvs map[string]map[string]string) ([]string, error) {
	var result []string
	for _, name := range c.Env {
		vars, ok := namedEnvs[name]
		if !ok {
			return nil, fmt.Errorf("environment %q referenced in container config not found", name)
		}
		for k, v := range vars {
			result = append(result, strings.ToUpper(k)+"="+v)
		}
	}
	return result, nil
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

func (c *ContainerConfig) ContainerPort() (string, error) {
	switch c.Type {
	case EmulatorAWS, EmulatorSnowflake:
		return DefaultAWSPort + "/tcp", nil
	default:
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
}

func (c *ContainerConfig) DisplayName() string {
	return c.Type.DisplayName()
}

func (c *ContainerConfig) ProductName() (string, error) {
	for _, e := range knownImages {
		if e.Default && e.Type == c.Type {
			return e.ProductName, nil
		}
	}
	return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
}
