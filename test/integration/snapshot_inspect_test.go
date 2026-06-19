package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeInspectFixture builds a .snapshot ZIP (stored entries, so compressed and
// uncompressed sizes equal the byte lengths) for the inspect command to read.
func writeInspectFixture(t *testing.T, path string, entries map[string]int) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		require.NoError(t, err)
		_, err = w.Write(bytes.Repeat([]byte("x"), entries[name]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
}

func TestSnapshotInspectLocalFileWithoutDocker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "demo.snapshot")
	writeInspectFixture(t, path, map[string]int{
		"api_states/000000000000/s3/us-east-1/store.state.avro":       100,
		"api_states/000000000000/dynamodb/us-east-1/store.state.avro": 50,
		"assets/ecr/layer1": 4000,
		"assets/s3/obj1":    1000,
	})

	stdout, stderr, err := runLstk(t, testContext(t), dir,
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "inspect", path,
	)
	require.NoError(t, err, "lstk snapshot inspect failed: %s", stderr)

	// Flat per-service breakdown, sorted largest-first; each service combines
	// its control-plane state and data assets (s3 = 100 state + 1000 data).
	assert.Contains(t, stdout, "ecr")
	assert.Contains(t, stdout, "s3")
	assert.Contains(t, stdout, "dynamodb")
	assert.Less(t, strings.Index(stdout, "ecr"), strings.Index(stdout, "s3"),
		"ecr (4000) should sort before s3 (1100)")
	assert.Contains(t, stdout, "TOTAL")
	assert.Contains(t, stdout, "100%")
}

func TestSnapshotInspectJSONWithoutDocker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "demo.snapshot")
	writeInspectFixture(t, path, map[string]int{
		"assets/ecr/layer1": 4000,
		"api_states/000000000000/s3/us-east-1/store.state.avro": 100,
	})

	stdout, stderr, err := runLstk(t, testContext(t), dir,
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "inspect", path, "--json",
	)
	require.NoError(t, err, "lstk snapshot inspect --json failed: %s", stderr)

	var got struct {
		Path              string `json:"path"`
		TotalUncompressed int64  `json:"total_uncompressed_bytes"`
		Services          []struct {
			Service      string `json:"service"`
			Uncompressed int64  `json:"uncompressed_bytes"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout was not valid JSON:\n%s", stdout)
	assert.Equal(t, int64(4100), got.TotalUncompressed)
	require.NotEmpty(t, got.Services)
	assert.Equal(t, "ecr", got.Services[0].Service)
}

func TestSnapshotInspectRejectsPodRef(t *testing.T) {
	t.Parallel()

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "inspect", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, strings.ToLower(stderr), "local")
	assert.Contains(t, stderr, "snapshot show")
}

func TestSnapshotInspectInvalidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.snapshot")
	require.NoError(t, os.WriteFile(path, []byte("not a zip"), 0o600))

	stdout, _, err := runLstk(t, testContext(t), dir,
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "inspect", path,
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not a valid snapshot archive")
}
