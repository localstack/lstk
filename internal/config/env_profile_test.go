package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAndLoad(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(viper.Reset)
	return path
}

func TestSetEnvProfileVar(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty env list",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = []\n",
			want: "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"p\"]\n\n[env.p]\nKEY = \"1\"\n",
		},
		{
			name: "env list holding only a comment",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [ # none yet\n]\n",
			want: "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"p\", # none yet\n]\n\n[env.p]\nKEY = \"1\"\n",
		},
		{
			name: "indented block without a type line",
			in:   "[[containers]]\n  port = \"4566\"\n",
			want: "[[containers]]\nenv = [\"p\"]\n  port = \"4566\"\n\n[env.p]\nKEY = \"1\"\n",
		},
		{
			name: "existing quoted table with the key in another case",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = ['p']\n\n[env.\"p\"]  # mine\nkey = 0\nOTHER = \"x\"\n",
			want: "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = ['p']\n\n[env.\"p\"]  # mine\nkey = \"1\"\nOTHER = \"x\"\n",
		},
		{
			name: "existing table without the key",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n[env.p]\nOTHER = \"x\"\n",
			want: "[[containers]]\ntype = \"aws\"\nenv = [\"p\"]\nport = \"4566\"\n\n[env.p]\nKEY = \"1\"\nOTHER = \"x\"\n",
		},
		{
			name: "only the first containers block is edited",
			in:   "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4567\"\nenv = [\"q\"]\n\n[env.q]\nA = \"b\"\n",
			want: "[[containers]]\ntype = \"aws\"\nenv = [\"p\"]\nport = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4567\"\nenv = [\"q\"]\n\n[env.q]\nA = \"b\"\n\n[env.p]\nKEY = \"1\"\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeAndLoad(t, tc.in)

			require.NoError(t, SetEnvProfileVar("p", "KEY", "1"))

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestSetEnvProfileVar_RollsBackWhenALaterProfileOverrides(t *testing.T) {
	original := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"p\", \"later\"]\n\n[env.p]\nKEY = \"0\"\n\n[env.later]\nKEY = \"0\"\n"
	path := writeAndLoad(t, original)

	err := SetEnvProfileVar("p", "KEY", "1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "[env.later]")
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(got))
}

func TestSetEnvProfileVar_RollsBackWhenResultDoesNotParse(t *testing.T) {
	// A dotted key already defines the profile, so appending a table header
	// would define it twice.
	original := "env.p.OTHER = \"x\"\n\n[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	path := writeAndLoad(t, original)

	require.Error(t, SetEnvProfileVar("p", "KEY", "1"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(got))
}

func TestSetEnvProfileVar_NoConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	assert.Error(t, SetEnvProfileVar("p", "KEY", "1"))
}
