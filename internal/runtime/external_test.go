package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalRuntime(t *testing.T) {
	rt := NewExternalRuntime("localstack-aws")
	ctx := context.Background()

	t.Run("is always healthy", func(t *testing.T) {
		assert.NoError(t, rt.IsHealthy(ctx))
	})

	t.Run("reports the matching container name as running", func(t *testing.T) {
		running, err := rt.IsRunning(ctx, "localstack-aws")
		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("reports any other name as not running", func(t *testing.T) {
		running, err := rt.IsRunning(ctx, "some-other-container")
		require.NoError(t, err)
		assert.False(t, running)
	})

	t.Run("lifecycle operations are unsupported", func(t *testing.T) {
		_, err := rt.GetBoundPort(ctx, "localstack-aws", "4566/tcp")
		assert.ErrorIs(t, err, ErrExternalRuntimeUnsupported)

		_, err = rt.ContainerStartedAt(ctx, "localstack-aws")
		assert.ErrorIs(t, err, ErrExternalRuntimeUnsupported)

		_, err = rt.ContainerEnv(ctx, "localstack-aws")
		assert.ErrorIs(t, err, ErrExternalRuntimeUnsupported)

		err = rt.Stop(ctx, "localstack-aws")
		assert.ErrorIs(t, err, ErrExternalRuntimeUnsupported)

		_, _, err = rt.Start(ctx, ContainerConfig{})
		assert.ErrorIs(t, err, ErrExternalRuntimeUnsupported)
	})
}
