package container

import (
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestEmulatorURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		emulator config.EmulatorType
		host     string
		want     string
	}{
		{config.EmulatorAWS, "localhost.localstack.cloud:4566", "http://localhost.localstack.cloud:4566"},
		{config.EmulatorAWS, "127.0.0.1:4566", "http://127.0.0.1:4566"},
		{config.EmulatorSnowflake, "localhost.localstack.cloud:4566", "http://snowflake.localhost.localstack.cloud:4566"},
		{config.EmulatorSnowflake, "127.0.0.1:4566", "http://127.0.0.1:4566"},
		{config.EmulatorAzure, "localhost.localstack.cloud:4566", "https://azure.localhost.localstack.cloud:4566"},
		{config.EmulatorAzure, "127.0.0.1:4566", "https://127.0.0.1:4566"},
		{config.EmulatorAzure, "[::1]:4566", "https://[::1]:4566"},
		{config.EmulatorType("unknown"), "localhost.localstack.cloud:4566", "http://localhost.localstack.cloud:4566"},
	}
	for _, tt := range tests {
		t.Run(string(tt.emulator)+"/"+tt.host, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, EmulatorURL(tt.emulator, tt.host))
		})
	}
}
