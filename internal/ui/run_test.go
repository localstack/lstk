package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveAuthThenSelectEmulator_OrdersAuthFirst verifies that on first run
// with no token, the login prompt fires before the emulator-selection prompt.
// This is the property that lets users authenticate before configuring an
// emulator instead of the other way around.
func TestResolveAuthThenSelectEmulator_OrdersAuthFirst(t *testing.T) {
	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configPath := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`[[containers]]
type = "aws"
port = "4566"
`), 0644))
	require.NoError(t, config.InitFromPath(configPath))
	t.Cleanup(func() { viper.Reset() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var mu sync.Mutex
	var prompts []string
	sink := output.SinkFunc(func(ev output.Event) {
		req, ok := ev.(output.UserInputRequestEvent)
		if !ok {
			return
		}
		mu.Lock()
		prompts = append(prompts, req.Prompt)
		mu.Unlock()
		req.ResponseCh <- output.InputResponse{SelectedKey: req.Options[0].Key}
	})

	platformClient := api.NewPlatformClient(mockServer.URL, log.Nop())
	opts := RunOptions{
		StartOptions: container.StartOptions{
			PlatformClient:   platformClient,
			ForceFileKeyring: true,
			WebAppURL:        mockServer.URL,
			Logger:           log.Nop(),
		},
	}

	require.NoError(t, resolveAuthToken(ctx, sink, &opts))
	assert.NotEmpty(t, opts.StartOptions.AuthToken, "token should be populated on opts after login")

	_, err := container.SelectEmulator(ctx, sink, "")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(prompts), 2, "expected at least the auth and emulator prompts")
	assert.True(t, strings.Contains(strings.ToLower(prompts[0]), "authorization"),
		"first prompt should be the login flow, got %q", prompts[0])
	assert.True(t, strings.Contains(strings.ToLower(prompts[1]), "emulator"),
		"second prompt should be emulator selection, got %q", prompts[1])
}
