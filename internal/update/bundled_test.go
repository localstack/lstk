package update

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingSetMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		present  []string
		expected []string
		want     []string
	}{
		{
			name:     "release ships no bundle",
			present:  []string{"lstk"},
			expected: nil,
			want:     nil,
		},
		{
			name:     "complete set",
			present:  []string{"lstk", "bundled-extensions", "lstk-extensions.toml"},
			expected: []string{"bundled-extensions", "lstk-extensions.toml"},
			want:     nil,
		},
		{
			// The transition case: a pre-bundling updater installed only lstk.
			name:     "nothing but lstk after crossing the transition",
			present:  []string{"lstk"},
			expected: []string{"bundled-extensions", "lstk-extensions.toml"},
			want:     []string{"bundled-extensions", "lstk-extensions.toml"},
		},
		{
			name:     "one member missing",
			present:  []string{"lstk", "bundled-extensions"},
			expected: []string{"bundled-extensions", "lstk-extensions.toml"},
			want:     []string{"lstk-extensions.toml"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range tc.present {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755))
			}
			assert.Equal(t, tc.want, missingSetMembers(dir, tc.expected))
		})
	}
}

// newGitHubServerWithCleanup reuses the package's one mock GitHub
// latest-release server (newTestGitHubServer in notify_test.go) and ties its
// shutdown to the test, so this file cannot grow a drifting second mock.
func newGitHubServerWithCleanup(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	srv := newTestGitHubServer(t, tag)
	t.Cleanup(srv.Close)
	return srv
}

// formattedLine renders an event the way the plain sink would.
func formattedLine(t *testing.T, event output.Event) string {
	t.Helper()
	line, ok := output.FormatEventLine(event)
	require.True(t, ok, "event should have a plain-text rendering")
	return line
}

// checkedEvent returns the single UpdateCheckedEvent a check emitted.
func checkedEvent(t *testing.T, events []output.Event) output.UpdateCheckedEvent {
	t.Helper()
	for _, e := range events {
		if checked, ok := e.(output.UpdateCheckedEvent); ok {
			return checked
		}
	}
	t.Fatal("no UpdateCheckedEvent was emitted")
	return output.UpdateCheckedEvent{}
}

// TestCheckRepairsIncompleteSetAtCurrentVersion is the repair half of the
// transition: the binary is already the latest version, so the version
// comparison alone would report "already up to date" and leave the user
// without their bundled extensions until another release ships.
func TestCheckRepairsIncompleteSetAtCurrentVersion(t *testing.T) {
	srv := newGitHubServerWithCleanup(t, "v1.2.3")
	t.Setenv(githubAPIEndpointEnv, srv.URL)

	var events []output.Event
	sink := output.SinkFunc(func(e output.Event) { events = append(events, e) })
	latest, available, repair, err := checkWithVersion(context.Background(), sink, "", "1.2.3", func() []string {
		return []string{"bundled-extensions", "lstk-extensions.toml"}
	})

	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", latest)
	assert.True(t, available, "an incomplete set must not short-circuit on the version")
	assert.True(t, repair, "Check must hand the repair fact to its caller, not have it re-derived")

	event := checkedEvent(t, events)
	assert.True(t, event.Available)
	assert.True(t, event.RepairBundled, "the check must say it is repairing, not that an upgrade is available")
	assert.NotContains(t, formattedLine(t, event), "Already up to date")
}

// TestCheckReportsUpToDateWhenSetIsComplete keeps the ordinary path as cheap
// as it was: same version, complete set, nothing to do and nothing downloaded.
func TestCheckReportsUpToDateWhenSetIsComplete(t *testing.T) {
	srv := newGitHubServerWithCleanup(t, "v1.2.3")
	t.Setenv(githubAPIEndpointEnv, srv.URL)

	var events []output.Event
	sink := output.SinkFunc(func(e output.Event) { events = append(events, e) })
	_, available, repair, err := checkWithVersion(context.Background(), sink, "", "1.2.3", func() []string { return nil })

	require.NoError(t, err)
	assert.False(t, available)
	assert.False(t, repair)

	event := checkedEvent(t, events)
	assert.False(t, event.Available)
	assert.False(t, event.RepairBundled)
	assert.Contains(t, formattedLine(t, event), "Already up to date")
}

// TestCheckPrefersVersionUpgradeOverRepair keeps the two reasons distinct: when
// a newer release exists, this is an ordinary upgrade even if the set is also
// incomplete, and the installed set is repaired by that upgrade anyway.
func TestCheckPrefersVersionUpgradeOverRepair(t *testing.T) {
	srv := newGitHubServerWithCleanup(t, "v2.0.0")
	t.Setenv(githubAPIEndpointEnv, srv.URL)

	var events []output.Event
	sink := output.SinkFunc(func(e output.Event) { events = append(events, e) })
	_, available, repair, err := checkWithVersion(context.Background(), sink, "", "1.2.3", func() []string {
		return []string{"bundled-extensions"}
	})

	require.NoError(t, err)
	assert.True(t, available)
	assert.False(t, repair, "a version upgrade is not a repair even when members are also missing")

	event := checkedEvent(t, events)
	assert.False(t, event.RepairBundled)
	assert.Contains(t, formattedLine(t, event), "Update available: 1.2.3 → v2.0.0")
}

// TestCheckSkipsRepairForDevBuilds keeps a dev build out of the network path
// entirely, as before.
func TestCheckSkipsRepairForDevBuilds(t *testing.T) {
	var events []output.Event
	sink := output.SinkFunc(func(e output.Event) { events = append(events, e) })
	_, available, repair, err := checkWithVersion(context.Background(), sink, "", "dev", func() []string {
		return []string{"bundled-extensions"}
	})

	require.NoError(t, err)
	assert.False(t, available)
	assert.False(t, repair)
	assert.True(t, checkedEvent(t, events).DevBuild)
}

// TestMissingSetMembersRequiresUsableFiles: "present" must mean what the
// extension resolver means by resolvable. A file that exists but cannot run
// leaves the user exactly as stranded as an absent one.
func TestMissingSetMembersRequiresUsableFiles(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("no exec bits on Windows")
	}

	t.Run("non-executable binary counts as missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bundled-extensions"), []byte("x"), 0o644))
		got := missingSetMembers(dir, []string{"bundled-extensions"})
		assert.Equal(t, []string{"bundled-extensions"}, got)
	})

	t.Run("directory squatting on the name counts as missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "bundled-extensions"), 0o755))
		got := missingSetMembers(dir, []string{"bundled-extensions"})
		assert.Equal(t, []string{"bundled-extensions"}, got)
	})

	t.Run("descriptions file needs no exec bit", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-extensions.toml"), []byte("x"), 0o644))
		got := missingSetMembers(dir, []string{"lstk-extensions.toml"})
		assert.Nil(t, got)
	})
}

// TestMissingSetMembersIgnoresUnreadableDir: a stat failure that is not
// "does not exist" must not count as missing. Treating a transient permission
// or I/O error as absence would trigger a pointless full re-download on every
// run.
func TestMissingSetMembersIgnoresUnreadableDir(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("directory permission bits do not gate stat on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "inner")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bundled-extensions"), []byte("x"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	got := missingSetMembers(dir, []string{"bundled-extensions"})
	assert.Nil(t, got, "an unreadable install dir is not evidence of a missing member")
}
