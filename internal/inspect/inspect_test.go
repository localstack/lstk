package inspect

import (
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnabledOnStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		profiles []string
		env      map[string]map[string]string
		want     bool
	}{
		{"unset", nil, nil, false},
		{"one", []string{"a"}, map[string]map[string]string{"a": {"app_inspector": "1"}}, true},
		{"true in any case with spaces", []string{"a"}, map[string]map[string]string{"a": {"app_inspector": " TRUE "}}, true},
		{"other truthy-looking words are off", []string{"a"}, map[string]map[string]string{"a": {"app_inspector": "yes"}}, false},
		{"last profile wins", []string{"a", "b"}, map[string]map[string]string{"a": {"app_inspector": "1"}, "b": {"app_inspector": "0"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &LocalConfig{Container: config.ContainerConfig{Type: config.EmulatorAWS, Env: tc.profiles}, Env: tc.env}

			got, err := enabledOnStart(cfg)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
