package mcpconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyServerEntryNewFile(t *testing.T) {
	out, err := applyServerEntry(nil, []string{"mcpServers"}, "localstack", map[string]any{"command": "docker"})
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	servers := root["mcpServers"].(map[string]any)
	entry := servers["localstack"].(map[string]any)
	assert.Equal(t, "docker", entry["command"])
}

func TestApplyServerEntryPreservesOtherServers(t *testing.T) {
	existing := []byte(`{"mcpServers":{"other":{"command":"foo"}},"unrelated":true}`)
	out, err := applyServerEntry(existing, []string{"mcpServers"}, "localstack", map[string]any{"command": "docker"})
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	servers := root["mcpServers"].(map[string]any)
	assert.Contains(t, servers, "other", "existing servers must be preserved")
	assert.Contains(t, servers, "localstack")
	assert.Equal(t, true, root["unrelated"], "unrelated top-level keys must be preserved")
}

func TestApplyServerEntryNestedRootPath(t *testing.T) {
	out, err := applyServerEntry([]byte(`{}`), []string{"servers"}, "localstack", map[string]any{"type": "stdio"})
	require.NoError(t, err)

	var root map[string]any
	require.NoError(t, json.Unmarshal(out, &root))
	assert.Contains(t, root, "servers")
}

func TestApplyServerEntryInvalidJSON(t *testing.T) {
	_, err := applyServerEntry([]byte(`{not json`), []string{"mcpServers"}, "localstack", nil)
	assert.Error(t, err)
}

func TestApplyServerEntryRefusesNonObjectKey(t *testing.T) {
	// An existing key of the wrong type must not be silently clobbered.
	_, err := applyServerEntry([]byte(`{"mcpServers":"oops"}`), []string{"mcpServers"}, "localstack", map[string]any{"command": "docker"})
	assert.Error(t, err)
}

func TestHasServerEntry(t *testing.T) {
	existing := []byte(`{"mcpServers":{"localstack":{"command":"docker"}}}`)
	assert.True(t, hasServerEntry(existing, []string{"mcpServers"}, "localstack"))
	assert.False(t, hasServerEntry(existing, []string{"mcpServers"}, "other"))
	assert.False(t, hasServerEntry(nil, []string{"mcpServers"}, "localstack"))
}
