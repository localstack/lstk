package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureSink struct {
	events []output.Event
}

func (s *captureSink) Emit(event output.Event) {
	s.events = append(s.events, event)
}

func cmdWithEndpointFlag(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String(endpoint.FlagName, "", "")
	return cmd
}

func TestRejectEndpointURL(t *testing.T) {
	t.Run("no source present is a no-op", func(t *testing.T) {
		cmd := cmdWithEndpointFlag(t)
		sink := &captureSink{}
		err := rejectEndpointURL(cmd, sink, "restart")
		require.NoError(t, err)
		assert.Empty(t, sink.events)
	})

	t.Run("explicit flag is rejected", func(t *testing.T) {
		cmd := cmdWithEndpointFlag(t)
		require.NoError(t, cmd.Flags().Set(endpoint.FlagName, "http://localhost:4566"))
		sink := &captureSink{}
		err := rejectEndpointURL(cmd, sink, "restart")
		require.Error(t, err)
		assert.True(t, output.IsSilent(err))
		require.Len(t, sink.events, 1)
		errEvent, ok := sink.events[0].(output.ErrorEvent)
		require.True(t, ok)
		assert.Contains(t, errEvent.Title, "restart does not support --endpoint-url")
		assert.Contains(t, errEvent.Title, "--endpoint-url was passed")
	})

	t.Run("ambient LSTK_ENDPOINT_URL is rejected, not silently ignored", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://localhost:4566")
		cmd := cmdWithEndpointFlag(t)
		sink := &captureSink{}
		err := rejectEndpointURL(cmd, sink, "stop")
		require.Error(t, err)
		require.Len(t, sink.events, 1)
		errEvent, ok := sink.events[0].(output.ErrorEvent)
		require.True(t, ok)
		assert.Contains(t, errEvent.Title, "stop does not support LSTK_ENDPOINT_URL")
		assert.Contains(t, errEvent.Title, "LSTK_ENDPOINT_URL is set")
	})

	t.Run("ambient AWS_ENDPOINT_URL is rejected too", func(t *testing.T) {
		t.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
		cmd := cmdWithEndpointFlag(t)
		sink := &captureSink{}
		err := rejectEndpointURL(cmd, sink, "logs")
		require.Error(t, err)
		require.Len(t, sink.events, 1)
		errEvent, ok := sink.events[0].(output.ErrorEvent)
		require.True(t, ok)
		assert.Contains(t, errEvent.Title, "logs does not support AWS_ENDPOINT_URL")
		assert.Contains(t, errEvent.Title, "AWS_ENDPOINT_URL is set")
	})

	t.Run("start is rejected identically to the other lifecycle commands", func(t *testing.T) {
		t.Setenv("LSTK_ENDPOINT_URL", "http://localhost:4566")
		cmd := cmdWithEndpointFlag(t)
		sink := &captureSink{}
		err := rejectEndpointURL(cmd, sink, "start")
		require.Error(t, err)
		errEvent, ok := sink.events[0].(output.ErrorEvent)
		require.True(t, ok)
		assert.Contains(t, errEvent.Title, "start does not support LSTK_ENDPOINT_URL")
	})
}
