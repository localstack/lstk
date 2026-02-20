package config

import "fmt"

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
