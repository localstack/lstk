package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func awsConfig(snapshot string) *config.Config {
	return &config.Config{
		Containers: []config.ContainerConfig{
			{Type: config.EmulatorAWS, Tag: "latest", Port: "4566", Snapshot: snapshot},
		},
	}
}

func TestResolveStartSnapshotRef(t *testing.T) {
	t.Run("from config", func(t *testing.T) {
		ref, err := resolveStartSnapshotRef(awsConfig("pod:my-baseline"), "", false)
		require.NoError(t, err)
		assert.Equal(t, "pod:my-baseline", ref)
	})

	t.Run("flag overrides config", func(t *testing.T) {
		ref, err := resolveStartSnapshotRef(awsConfig("pod:my-baseline"), "pod:override", false)
		require.NoError(t, err)
		assert.Equal(t, "pod:override", ref)
	})

	t.Run("no-snapshot skips config", func(t *testing.T) {
		ref, err := resolveStartSnapshotRef(awsConfig("pod:my-baseline"), "", true)
		require.NoError(t, err)
		assert.Equal(t, "", ref)
	})

	t.Run("no snapshot configured", func(t *testing.T) {
		ref, err := resolveStartSnapshotRef(awsConfig(""), "", false)
		require.NoError(t, err)
		assert.Equal(t, "", ref)
	})

	t.Run("snapshot only read from AWS container", func(t *testing.T) {
		cfg := &config.Config{Containers: []config.ContainerConfig{
			{Type: config.EmulatorSnowflake, Tag: "latest", Port: "4566", Snapshot: "pod:ignored"},
		}}
		ref, err := resolveStartSnapshotRef(cfg, "", false)
		require.NoError(t, err)
		assert.Equal(t, "", ref)
	})

	t.Run("conflicting flags error", func(t *testing.T) {
		_, err := resolveStartSnapshotRef(awsConfig(""), "pod:x", true)
		assert.ErrorContains(t, err, "cannot be used together")
	})
}

func TestSnapshotStartFlagsRegistered(t *testing.T) {
	root := NewRootCmd(&env.Env{}, telemetry.New("", true), log.Nop())

	assert.NotNil(t, root.Flags().Lookup("snapshot"), "root should register --snapshot")
	assert.NotNil(t, root.Flags().Lookup("no-snapshot"), "root should register --no-snapshot")

	start, _, err := root.Find([]string{"start"})
	require.NoError(t, err)
	assert.NotNil(t, start.Flags().Lookup("snapshot"), "start should register --snapshot")
	assert.NotNil(t, start.Flags().Lookup("no-snapshot"), "start should register --no-snapshot")
}
