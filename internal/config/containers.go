package config

import (
	"fmt"
	"strings"
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

var emulatorDisplayNames = map[EmulatorType]string{
	EmulatorAWS:       "AWS",
	EmulatorSnowflake: "Snowflake",
	EmulatorAzure:     "Azure",
}

var emulatorImages = map[EmulatorType]string{
	EmulatorAWS: "localstack-pro",
}

var emulatorHealthPaths = map[EmulatorType]string{
	EmulatorAWS: "/_localstack/health",
}

type ContainerConfig struct {
	Type EmulatorType `mapstructure:"type"`
	Tag  string       `mapstructure:"tag"`
	Port string       `mapstructure:"port"`
	// Env is a list of named environment references defined in the top-level [env.*] config sections.
	Env []string `mapstructure:"env"`
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

func (c *ContainerConfig) DisplayName() string {
	name, ok := emulatorDisplayNames[c.Type]
	if !ok {
		return fmt.Sprintf("LocalStack %s Emulator", c.Type)
	}
	return fmt.Sprintf("LocalStack %s Emulator", name)
}

func (c *ContainerConfig) ProductName() (string, error) {
	productName, ok := emulatorImages[c.Type]
	if !ok {
		return "", fmt.Errorf("%s emulator not supported yet by lstk", c.Type)
	}
	return productName, nil
}
