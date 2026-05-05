package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwitchEmulatorContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		content     string
		to          EmulatorType
		wantChanged bool
		contains    []string
		notContains []string
	}{
		{
			name:        "no-op when already aws",
			content:     "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n",
			to:          EmulatorAWS,
			wantChanged: false,
		},
		{
			name:        "no-op when already snowflake",
			content:     "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n",
			to:          EmulatorSnowflake,
			wantChanged: false,
		},
		{
			name: "comments aws block and appends snowflake",
			content: `[[containers]]
type = "aws"
port = "4566"

[cli]
update_skipped_version = ""
`,
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{"# [[containers]]", `# type = "aws"`, `# port = "4566"`, `type = "snowflake"`, "[cli]"},
			notContains: []string{"[[containers]]\ntype = \"aws\""},
		},
		{
			name:        "restores commented aws block",
			content:     "# [[containers]]\n# type = \"aws\"\n# port = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n",
			to:          EmulatorAWS,
			wantChanged: true,
			contains:    []string{"[[containers]]\ntype = \"aws\"", "# [[containers]]", `# type = "snowflake"`},
			notContains: []string{"[[containers]]\ntype = \"snowflake\""},
		},
		{
			name:        "restores commented snowflake block",
			content:     "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n# [[containers]]\n# type = \"snowflake\"\n# port = \"4566\"\n",
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{"[[containers]]\ntype = \"snowflake\"", "# [[containers]]", `# type = "aws"`},
		},
		{
			name: "preserves non-container content",
			content: `# lstk configuration file

[[containers]]
type = "aws"
port = "4566"
# volume = ""    # some comment

# [env.debug]
# DEBUG = "1"

[cli]
update_skipped_version = "v1.2.3"
`,
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{"# lstk configuration file", `update_skipped_version = "v1.2.3"`, "# [env.debug]", `type = "snowflake"`},
		},
		{
			// Original inline comments should be preserved in the commented-out block
			name:        "preserves inline comments when commenting out block",
			content:     "[[containers]]\ntype = \"aws\"     # Emulator type\ntag  = \"latest\"  # Docker image tag\nport = \"4566\"    # Host port\n# volume = \"\"    # persistent state\n",
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{"# type = \"aws\"     # Emulator type", "# # volume = \"\"    # persistent state"},
		},
		{
			name:        "single-quoted type is recognized",
			content:     "[[containers]]\ntype = 'aws'\nport = \"4566\"\n",
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{`type = "snowflake"`},
		},
		{
			// detectBlockType must not match a commented-out type line inside an active block
			name:        "commented type line within active block is ignored",
			content:     "[[containers]]\n# type = \"snowflake\"\ntype = \"aws\"\nport = \"4566\"\n",
			to:          EmulatorSnowflake,
			wantChanged: true,
			contains:    []string{`type = "snowflake"`},
			notContains: []string{"[[containers]]\ntype = \"aws\""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, changed := switchEmulatorContent(tc.content, tc.to)
			assert.Equal(t, tc.wantChanged, changed)
			if !tc.wantChanged {
				assert.Equal(t, tc.content, result)
			}
			for _, s := range tc.contains {
				assert.Contains(t, result, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, result, s)
			}
		})
	}
}

func TestSwitchEmulatorContent_RoundTrip(t *testing.T) {
	t.Parallel()
	original := `[[containers]]
type = "aws"
port = "4566"
`
	// Switch to snowflake
	afterSnowflake, changed := switchEmulatorContent(original, EmulatorSnowflake)
	assert.True(t, changed)
	assert.Contains(t, afterSnowflake, `type = "snowflake"`)

	// Switch back to AWS — should restore the commented block
	afterAWS, changed := switchEmulatorContent(afterSnowflake, EmulatorAWS)
	assert.True(t, changed)
	assert.Contains(t, afterAWS, "[[containers]]\ntype = \"aws\"")
	assert.NotContains(t, afterAWS, "\n[[containers]]\ntype = \"snowflake\"")
}

func TestSwitchEmulator_WritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SwitchEmulator(EmulatorSnowflake))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "snowflake"`)
	assert.True(t, strings.Contains(string(got), "# [[containers]]"))

	cfg, err := Get()
	require.NoError(t, err)
	require.Len(t, cfg.Containers, 1)
	assert.Equal(t, EmulatorSnowflake, cfg.Containers[0].Type)
}

func TestSwitchEmulator_NoOpWhenSameEmulator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SwitchEmulator(EmulatorAWS))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}
