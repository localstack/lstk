package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoadConfig feeds arbitrary bytes as a TOML config file to the config parser.
// Targets: viper TOML parsing, ContainerConfig.Validate(), env resolution.
func FuzzLoadConfig(f *testing.F) {
	// Seed with valid configs
	f.Add([]byte(`[[containers]]
type = "aws"
tag  = "stable"
port = "4566"
`))
	f.Add([]byte(`[[containers]]
type = "aws"
tag  = "stable"
port = "4566"
env  = ["debug"]

[env.debug]
DEBUG = "1"
`))
	f.Add([]byte(`[[containers]]
type = "aws"
port = "1"

[[containers]]
type = "aws"
port = "65535"
`))
	f.Add([]byte(``))
	f.Add([]byte(`[[containers]]
type = "nonexistent"
port = "4566"
`))
	f.Add([]byte(`[[containers]]
type = "aws"
port = "0"
`))
	f.Add([]byte(`[[containers]]
type = "aws"
port = "99999999999999999999"
tag = "` + string(make([]byte, 256)) + `"
`))

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}

		// Parse the config — should never panic
		_ = InitFromPath(path)

		// If parsing succeeded, try to get and validate the config
		cfg, err := Get()
		if err != nil {
			return
		}

		// Exercise container config methods
		for i := range cfg.Containers {
			c := &cfg.Containers[i]
			_ = c.Validate()
			_, _ = c.Image()
			_ = c.Name()
			_, _ = c.VolumeDir()
			_, _ = c.HealthPath()
			_, _ = c.ContainerPort()
			_ = c.DisplayName()
			_, _ = c.ProductName()
			_, _ = c.ResolvedEnv(cfg.Env)
		}
	})
}

// FuzzContainerConfigValidate directly fuzzes the Validate method with arbitrary port strings.
func FuzzContainerConfigValidate(f *testing.F) {
	f.Add("aws", "stable", "4566", "/tmp/vol")
	f.Add("aws", "", "0", "")
	f.Add("snowflake", "latest", "65535", "")
	f.Add("azure", "nightly", "-1", "/etc/passwd")
	f.Add("", "", "", "")
	f.Add("aws", "../../etc/passwd", "99999", "$(whoami)")

	f.Fuzz(func(t *testing.T, emType, tag, port, volume string) {
		c := &ContainerConfig{
			Type:   EmulatorType(emType),
			Tag:    tag,
			Port:   port,
			Volume: volume,
		}

		// None of these should panic
		_ = c.Validate()
		_, _ = c.Image()
		_ = c.Name()
		_, _ = c.VolumeDir()
		_ = c.DisplayName()
		_, _ = c.ProductName()
		_, _ = c.HealthPath()
		_, _ = c.ContainerPort()
	})
}
