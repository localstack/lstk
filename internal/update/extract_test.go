package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both release archive formats, since the two extractors are separate code.
var archiveFormats = []string{"tar.gz", "zip"}

type archiveEntry struct {
	name string
	body string
	mode os.FileMode
	link string // when set, a symlink entry pointing at link (never shipped; for extractor tests)
}

func lstkBinaryName() string { return exeName("lstk", goruntime.GOOS) }
func bundleName() string     { return bundledBinaryName(goruntime.GOOS) }

func buildArchive(t *testing.T, format string, entries []archiveEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive."+format)
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	if format == "zip" {
		zw := zip.NewWriter(f)
		for _, e := range entries {
			hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
			body := e.body
			if e.link != "" {
				hdr.SetMode(e.mode | os.ModeSymlink)
				body = e.link
			} else {
				hdr.SetMode(e.mode)
			}
			w, err := zw.CreateHeader(hdr)
			require.NoError(t, err)
			_, err = w.Write([]byte(body))
			require.NoError(t, err)
		}
		require.NoError(t, zw.Close())
		return path
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		if e.link != "" {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Mode: int64(e.mode), Linkname: e.link, Typeflag: tar.TypeSymlink}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Mode: int64(e.mode), Size: int64(len(e.body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(e.body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return path
}

// newInstallDir writes files into a fresh directory and returns it with the
// path of the lstk binary. Everything but the descriptions file is 0755.
func newInstallDir(t *testing.T, files map[string]string) (dir, exePath string) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		mode := os.FileMode(0o755)
		if name == descriptionsFileName {
			mode = 0o644
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), mode))
	}
	return dir, filepath.Join(dir, lstkBinaryName())
}

func requireFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err, "expected %s to exist", path)
	assert.Equal(t, want, string(got), "content of %s", path)
}

func requireExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	if goruntime.GOOS != "windows" {
		assert.NotZero(t, info.Mode().Perm()&0o111, "%s should be executable", path)
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	assert.True(t, os.IsNotExist(err), "%s should not exist", path)
}

func requireNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == stagingSuffix && e.Type().IsRegular() {
			t.Errorf("staging leftover: %s", e.Name())
		}
	}
}

// TestExtractAndReplaceInstallsArchiveSet covers the archive shapes the
// updater has to handle, in both formats. The set is lstk, the
// bundled-extensions binary and the descriptions file; anything else at the
// archive root is ignored, and files beside lstk are never deleted.
func TestExtractAndReplaceInstallsArchiveSet(t *testing.T) {
	t.Parallel()
	toml := descriptionsFileName
	cases := []struct {
		name       string
		installed  map[string]string
		archive    []archiveEntry
		want       map[string]string // content under the real names after the update
		executable []string
		absent     []string // must not have been installed
	}{
		{
			name:      "the whole set replaces all three members",
			installed: map[string]string{lstkBinaryName(): "old lstk", bundleName(): "old bundle", toml: "doctor = \"old\"\n"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundleName(), body: "new bundle", mode: 0o755},
				{name: toml, body: "doctor = \"new\"\ndeploy = \"new\"\n", mode: 0o644},
			},
			want:       map[string]string{lstkBinaryName(): "new lstk", bundleName(): "new bundle", toml: "doctor = \"new\"\ndeploy = \"new\"\n"},
			executable: []string{lstkBinaryName(), bundleName()},
		},
		{
			name:      "the bundle is added to a pre-bundling install",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundleName(), body: "bundle", mode: 0o755},
				{name: toml, body: "deploy = \"Deploy\"\n", mode: 0o644},
			},
			want:       map[string]string{lstkBinaryName(): "new lstk", bundleName(): "bundle", toml: "deploy = \"Deploy\"\n"},
			executable: []string{bundleName()},
		},
		{
			name:      "bundle without a toml still installs",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundleName(), body: "bundle", mode: 0o755},
			},
			want: map[string]string{lstkBinaryName(): "new lstk", bundleName(): "bundle"},
		},
		{
			// The pre-bundling and rollback shape: previously installed files stay.
			name:      "lstk-only archive replaces lstk and keeps the rest",
			installed: map[string]string{lstkBinaryName(): "old lstk", bundleName(): "bundle", toml: "doctor = \"Doctor\"\n", "lstk-mine": "user extension"},
			archive:   []archiveEntry{{name: lstkBinaryName(), body: "new lstk", mode: 0o755}},
			want:      map[string]string{lstkBinaryName(): "new lstk", bundleName(): "bundle", toml: "doctor = \"Doctor\"\n", "lstk-mine": "user extension"},
		},
		{
			// Release archives carry completions and manpages too; an executable
			// lstk-* file is not part of the set either (decision 7b).
			name:      "other files at the archive root are not installed",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: exeName("lstk-alpha", goruntime.GOOS), body: "standalone extension", mode: 0o755},
				{name: "lstk-notes.txt", body: "notes", mode: 0o644},
				{name: "README.md", body: "readme", mode: 0o644},
			},
			want:   map[string]string{lstkBinaryName(): "new lstk"},
			absent: []string{exeName("lstk-alpha", goruntime.GOOS), "lstk-notes.txt", "README.md"},
		},
	}
	for _, tc := range cases {
		for _, format := range archiveFormats {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				t.Parallel()
				dir, exePath := newInstallDir(t, tc.installed)
				require.NoError(t, extractAndReplace(buildArchive(t, format, tc.archive), exePath, format))
				for name, body := range tc.want {
					requireFileContent(t, filepath.Join(dir, name), body)
				}
				for _, name := range tc.executable {
					requireExecutable(t, filepath.Join(dir, name))
				}
				for _, name := range tc.absent {
					requireAbsent(t, filepath.Join(dir, name))
				}
				requireNoStagingLeftovers(t, dir)
			})
		}
	}
}

func TestExtractAndReplaceRejectsArchiveWithoutLstk(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	archive := buildArchive(t, "tar.gz", []archiveEntry{{name: bundleName(), body: "bundle", mode: 0o755}})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, "binary not found in archive")
	requireFileContent(t, exePath, "old lstk")
	requireNoStagingLeftovers(t, dir)
}

// A failure while staging leaves the installation untouched and no staging
// files behind. Here the toml's staging path is blocked by a directory after
// the bundle was already staged (members stage in name order, lstk last).
func TestExtractAndReplaceStagingFailureLeavesInstallUntouched(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk", bundleName(): "old bundle", descriptionsFileName: "old toml"})
	require.NoError(t, os.MkdirAll(filepath.Join(dir, descriptionsFileName+stagingSuffix), 0o755))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: bundleName(), body: "new bundle", mode: 0o755},
		{name: descriptionsFileName, body: "new toml", mode: 0o644},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, descriptionsFileName)
	require.ErrorContains(t, err, "move it out of the way")
	requireFileContent(t, exePath, "old lstk")
	requireFileContent(t, filepath.Join(dir, bundleName()), "old bundle")
	requireFileContent(t, filepath.Join(dir, descriptionsFileName), "old toml")
	requireNoStagingLeftovers(t, dir)
}

// Staging must never write through a symlink left at a staging path: that
// would destroy the target and install the link as the member.
func TestExtractAndReplaceRefusesSymlinkSquatter(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("os.Symlink needs elevation on Windows")
	}
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	target := filepath.Join(t.TempDir(), "precious")
	require.NoError(t, os.WriteFile(target, []byte("precious data"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, bundleName()+stagingSuffix)))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: bundleName(), body: "bundle", mode: 0o755},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, bundleName())
	requireFileContent(t, target, "precious data")
	requireFileContent(t, exePath, "old lstk")
	requireAbsent(t, filepath.Join(dir, bundleName()))
}

// A regular file appearing at a staging path after cleanup means another
// update is running; it must not be truncated.
func TestStageMembersRefusesExistingStagingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest := filepath.Join(dir, bundledBinaryBaseName)
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.WriteFile(src, []byte("bundle"), 0o755))
	require.NoError(t, os.WriteFile(dest+stagingSuffix, []byte("another update's bytes"), 0o755))
	require.Error(t, stageMembers([]updateMember{{src: src, dest: dest, mode: 0o755}}))
	requireFileContent(t, dest+stagingSuffix, "another update's bytes")
}

func TestExtractAndReplaceCleansLeftoverStagingFiles(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	for _, name := range []string{lstkBinaryName(), bundleName(), "gone"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+stagingSuffix), []byte("crashed"), 0o755))
	}
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: bundleName(), body: "bundle", mode: 0o755},
	})
	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))
	requireFileContent(t, exePath, "new lstk")
	requireFileContent(t, filepath.Join(dir, bundleName()), "bundle")
	requireAbsent(t, filepath.Join(dir, "gone"))
	requireNoStagingLeftovers(t, dir)
}

// Committing lstk last: a member that fails to commit leaves lstk on the
// previous version.
func TestExtractAndReplaceCommitFailureKeepsPreviousLstk(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	blocked := filepath.Join(dir, bundleName())
	require.NoError(t, os.MkdirAll(blocked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o644))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: bundleName(), body: "bundle", mode: 0o755},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, bundleName())
	requireFileContent(t, exePath, "old lstk")
}

// The install path is data, not a glob pattern.
func TestExtractAndReplaceWorksInGlobMetacharacterDir(t *testing.T) {
	t.Parallel()
	for _, dirName := range []string{"we[ird", "we[ir]d", "sta*rs", "quest?ion"} {
		t.Run(dirName, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), dirName)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			exePath := filepath.Join(dir, lstkBinaryName())
			require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
			leftover := filepath.Join(dir, bundleName()+stagingSuffix)
			require.NoError(t, os.WriteFile(leftover, []byte("crashed"), 0o755))
			archive := buildArchive(t, "tar.gz", []archiveEntry{{name: lstkBinaryName(), body: "new lstk", mode: 0o755}})
			require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))
			requireFileContent(t, exePath, "new lstk")
			requireAbsent(t, leftover)
		})
	}
}

func TestExtractAndReplacePreservesSpecialModeBits(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("no Unix mode bits on Windows")
	}
	_, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	require.NoError(t, os.Chmod(exePath, 0o755|os.ModeSetgid))
	if info, err := os.Stat(exePath); err != nil || info.Mode()&os.ModeSetgid == 0 {
		t.Skip("filesystem does not support setgid on files")
	}
	archive := buildArchive(t, "tar.gz", []archiveEntry{{name: lstkBinaryName(), body: "new lstk", mode: 0o755}})
	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))
	info, err := os.Stat(exePath)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSetgid, "setgid must survive the update, got %v", info.Mode())
}

// The Windows shape, exercised from any host through the goos parameter: zip,
// ".exe" names, and every existing member moved to ".old" before the rename.
func TestReplaceSetWindows(t *testing.T) {
	t.Parallel()
	t.Run("whole set with .exe names", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		exePath := filepath.Join(dir, "lstk.exe")
		require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bundled-extensions.exe"), []byte("old bundle"), 0o755))
		archive := buildArchive(t, "zip", []archiveEntry{
			{name: "lstk.exe", body: "new lstk", mode: 0o755},
			{name: "bundled-extensions.exe", body: "new bundle", mode: 0o755},
			{name: "bundled-extensions", body: "no .exe: not the Windows member", mode: 0o755},
			{name: "lstk-alpha.exe", body: "not part of the set", mode: 0o755},
			{name: descriptionsFileName, body: "deploy = \"Deploy\"\n", mode: 0o644},
		})
		require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))
		requireFileContent(t, exePath, "new lstk")
		requireFileContent(t, filepath.Join(dir, "lstk.exe.old"), "old lstk")
		requireFileContent(t, filepath.Join(dir, "bundled-extensions.exe"), "new bundle")
		requireFileContent(t, filepath.Join(dir, "bundled-extensions.exe.old"), "old bundle")
		requireFileContent(t, filepath.Join(dir, descriptionsFileName), "deploy = \"Deploy\"\n")
		requireAbsent(t, filepath.Join(dir, "bundled-extensions"))
		requireAbsent(t, filepath.Join(dir, "lstk-alpha.exe"))
		requireNoStagingLeftovers(t, dir)
	})
	t.Run("lstk-only archive refreshes .old and keeps installed members", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		exePath := filepath.Join(dir, "lstk.exe")
		require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
		require.NoError(t, os.WriteFile(exePath+".old", []byte("older lstk"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bundled-extensions.exe"), []byte("installed bundle"), 0o755))
		archive := buildArchive(t, "zip", []archiveEntry{{name: "lstk.exe", body: "new lstk", mode: 0o755}})
		require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))
		requireFileContent(t, exePath, "new lstk")
		requireFileContent(t, exePath+".old", "old lstk")
		requireFileContent(t, filepath.Join(dir, "bundled-extensions.exe"), "installed bundle")
	})
}

// When the final rename fails after lstk.exe was moved aside, it is moved back.
func TestCommitRestoresRunningBinaryOnWindowsRenameFailure(t *testing.T) {
	t.Parallel()
	dest := filepath.Join(t.TempDir(), "lstk.exe")
	require.NoError(t, os.WriteFile(dest, []byte("old lstk"), 0o755))
	m := updateMember{dest: dest, mode: 0o755} // no staging file: the rename fails
	require.Error(t, m.commit("windows"))
	requireFileContent(t, dest, "old lstk")
	requireAbsent(t, dest+".old")
}

// A zip symlink entry extracted as a file would be a "binary" holding a path
// string, which discoverMembers would then install as the bundle.
func TestExtractorsSkipSymlinkEntries(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryBaseName, mode: 0o755, link: lstkBinaryName()},
			})
			dest := t.TempDir()
			if format == "zip" {
				require.NoError(t, extractZip(archive, dest))
			} else {
				require.NoError(t, extractTarGz(archive, dest))
			}
			requireAbsent(t, filepath.Join(dest, bundledBinaryBaseName))
			requireFileContent(t, filepath.Join(dest, lstkBinaryName()), "new lstk")
		})
	}
}
