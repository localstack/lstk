package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireExternalPodAuth(t *testing.T) {
	t.Parallel()

	require.NoError(t, requireExternalPodAuth(false, ""), "a locally managed emulator may reuse its startup identity")
	require.NoError(t, requireExternalPodAuth(true, "test-token"), "an external emulator accepts explicit caller authentication")
	assert.ErrorContains(t, requireExternalPodAuth(true, ""), "authentication is required for cloud snapshot operations against an externally-managed emulator")
}
