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

func lstkBinaryName() string           { return exeName("lstk", goruntime.GOOS) }
func extBinaryName(name string) string { return exeName("lstk-"+name, goruntime.GOOS) }

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
// updater has to handle, in both formats. The set is whatever the archive
// contains; files the archive does not carry are never deleted.
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
			name:      "lstk plus two extensions plus toml replaces all of them",
			installed: map[string]string{lstkBinaryName(): "old lstk", extBinaryName("alpha"): "old alpha", extBinaryName("beta"): "old beta", toml: "alpha = \"old\"\n"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
				{name: extBinaryName("beta"), body: "new beta", mode: 0o755},
				{name: toml, body: "alpha = \"new\"\nbeta = \"new\"\n", mode: 0o644},
			},
			want:       map[string]string{lstkBinaryName(): "new lstk", extBinaryName("alpha"): "new alpha", extBinaryName("beta"): "new beta", toml: "alpha = \"new\"\nbeta = \"new\"\n"},
			executable: []string{lstkBinaryName(), extBinaryName("alpha"), extBinaryName("beta")},
		},
		{
			name:      "a new extension is installed",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: extBinaryName("deploy"), body: "new deploy", mode: 0o755},
				{name: toml, body: "deploy = \"Deploy\"\n", mode: 0o644},
			},
			want:       map[string]string{lstkBinaryName(): "new lstk", extBinaryName("deploy"): "new deploy", toml: "deploy = \"Deploy\"\n"},
			executable: []string{extBinaryName("deploy")},
		},
		{
			name:      "the multi-call bundle and its toml",
			installed: map[string]string{lstkBinaryName(): "old lstk", bundledBinaryName(goruntime.GOOS): "old bundle", toml: "doctor = \"Doctor\"\n"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryName(goruntime.GOOS), body: "new bundle", mode: 0o755},
				{name: toml, body: "doctor = \"Doctor\"\ndeploy = \"Deploy\"\n", mode: 0o644},
			},
			want:       map[string]string{lstkBinaryName(): "new lstk", bundledBinaryName(goruntime.GOOS): "new bundle", toml: "doctor = \"Doctor\"\ndeploy = \"Deploy\"\n"},
			executable: []string{bundledBinaryName(goruntime.GOOS)},
		},
		{
			name:      "bundle without a toml still installs",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryName(goruntime.GOOS), body: "bundle", mode: 0o755},
			},
			want: map[string]string{lstkBinaryName(): "new lstk", bundledBinaryName(goruntime.GOOS): "bundle"},
		},
		{
			// The pre-bundling and rollback shape: previously installed members stay.
			name:      "lstk-only archive replaces lstk and keeps the rest",
			installed: map[string]string{lstkBinaryName(): "old lstk", extBinaryName("alpha"): "installed alpha", bundledBinaryName(goruntime.GOOS): "bundle"},
			archive:   []archiveEntry{{name: lstkBinaryName(), body: "new lstk", mode: 0o755}},
			want:      map[string]string{lstkBinaryName(): "new lstk", extBinaryName("alpha"): "installed alpha", bundledBinaryName(goruntime.GOOS): "bundle"},
		},
		{
			name:      "a user's lstk-* file absent from the archive is left alone",
			installed: map[string]string{lstkBinaryName(): "old lstk", extBinaryName("mine"): "user extension"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: extBinaryName("deploy"), body: "new deploy", mode: 0o755},
			},
			want: map[string]string{extBinaryName("mine"): "user extension", extBinaryName("deploy"): "new deploy"},
		},
		{
			name:      "data files at the archive root are not installed",
			installed: map[string]string{lstkBinaryName(): "old lstk"},
			archive: []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: "lstk-notes.txt", body: "not an extension", mode: 0o644},
				{name: "README.md", body: "readme", mode: 0o644},
			},
			want:   map[string]string{lstkBinaryName(): "new lstk"},
			absent: []string{"lstk-notes.txt", "README.md"},
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
	archive := buildArchive(t, "tar.gz", []archiveEntry{{name: extBinaryName("alpha"), body: "alpha", mode: 0o755}})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, "binary not found in archive")
	requireFileContent(t, exePath, "old lstk")
	requireNoStagingLeftovers(t, dir)
}

// A failure while staging leaves the installation untouched and no staging
// files behind. Here beta's staging path is blocked by a directory after alpha
// was already staged.
func TestExtractAndReplaceStagingFailureLeavesInstallUntouched(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk", extBinaryName("alpha"): "old alpha", extBinaryName("beta"): "old beta"})
	require.NoError(t, os.MkdirAll(filepath.Join(dir, extBinaryName("beta")+stagingSuffix), 0o755))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
		{name: extBinaryName("beta"), body: "new beta", mode: 0o755},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, extBinaryName("beta"))
	require.ErrorContains(t, err, "move it out of the way")
	requireFileContent(t, exePath, "old lstk")
	requireFileContent(t, filepath.Join(dir, extBinaryName("alpha")), "old alpha")
	requireFileContent(t, filepath.Join(dir, extBinaryName("beta")), "old beta")
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
	require.NoError(t, os.Symlink(target, filepath.Join(dir, extBinaryName("alpha")+stagingSuffix)))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, extBinaryName("alpha"))
	requireFileContent(t, target, "precious data")
	requireFileContent(t, exePath, "old lstk")
	requireAbsent(t, filepath.Join(dir, extBinaryName("alpha")))
}

// A regular file appearing at a staging path after cleanup means another
// update is running; it must not be truncated.
func TestStageMembersRefusesExistingStagingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest := filepath.Join(dir, "lstk-alpha")
	src := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.WriteFile(src, []byte("new alpha"), 0o755))
	require.NoError(t, os.WriteFile(dest+stagingSuffix, []byte("another update's bytes"), 0o755))
	require.Error(t, stageMembers([]updateMember{{src: src, dest: dest, mode: 0o755}}))
	requireFileContent(t, dest+stagingSuffix, "another update's bytes")
}

func TestExtractAndReplaceCleansLeftoverStagingFiles(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	for _, name := range []string{lstkBinaryName(), extBinaryName("alpha"), extBinaryName("gone")} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+stagingSuffix), []byte("crashed"), 0o755))
	}
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
	})
	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))
	requireFileContent(t, exePath, "new lstk")
	requireFileContent(t, filepath.Join(dir, extBinaryName("alpha")), "new alpha")
	requireAbsent(t, filepath.Join(dir, extBinaryName("gone")))
	requireNoStagingLeftovers(t, dir)
}

// Committing lstk last: a member that fails to commit leaves lstk on the
// previous version.
func TestExtractAndReplaceCommitFailureKeepsPreviousLstk(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{lstkBinaryName(): "old lstk"})
	blocked := filepath.Join(dir, extBinaryName("alpha"))
	require.NoError(t, os.MkdirAll(blocked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o644))
	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
	})
	err := extractAndReplace(archive, exePath, "tar.gz")
	require.ErrorContains(t, err, extBinaryName("alpha"))
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
			leftover := filepath.Join(dir, extBinaryName("gone")+stagingSuffix)
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
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-alpha.exe"), []byte("old alpha"), 0o755))
		archive := buildArchive(t, "zip", []archiveEntry{
			{name: "lstk.exe", body: "new lstk", mode: 0o755},
			{name: "lstk-alpha.exe", body: "new alpha", mode: 0o755},
			{name: "lstk-beta.exe", body: "new beta", mode: 0o755},
			{name: "lstk-notes.txt", body: "not an extension", mode: 0o644},
			{name: "lstk-plain", body: "no .exe: not an extension on Windows", mode: 0o755},
			{name: descriptionsFileName, body: "alpha = \"Alpha\"\n", mode: 0o644},
		})
		require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))
		requireFileContent(t, exePath, "new lstk")
		requireFileContent(t, filepath.Join(dir, "lstk.exe.old"), "old lstk")
		requireFileContent(t, filepath.Join(dir, "lstk-alpha.exe"), "new alpha")
		requireFileContent(t, filepath.Join(dir, "lstk-alpha.exe.old"), "old alpha")
		requireFileContent(t, filepath.Join(dir, "lstk-beta.exe"), "new beta")
		requireAbsent(t, filepath.Join(dir, "lstk-beta.exe.old"))
		requireFileContent(t, filepath.Join(dir, descriptionsFileName), "alpha = \"Alpha\"\n")
		requireAbsent(t, filepath.Join(dir, "lstk-notes.txt"))
		requireAbsent(t, filepath.Join(dir, "lstk-plain"))
		requireNoStagingLeftovers(t, dir)
	})
	t.Run("lstk-only archive refreshes .old and keeps installed members", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		exePath := filepath.Join(dir, "lstk.exe")
		require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
		require.NoError(t, os.WriteFile(exePath+".old", []byte("older lstk"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-alpha.exe"), []byte("installed alpha"), 0o755))
		archive := buildArchive(t, "zip", []archiveEntry{{name: "lstk.exe", body: "new lstk", mode: 0o755}})
		require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))
		requireFileContent(t, exePath, "new lstk")
		requireFileContent(t, exePath+".old", "old lstk")
		requireFileContent(t, filepath.Join(dir, "lstk-alpha.exe"), "installed alpha")
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

// A zip symlink entry extracted as a file would be an executable holding a
// path string, which discoverMembers would install as an extension.
func TestExtractorsSkipSymlinkEntries(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryBaseName, body: "bundle", mode: 0o755},
				{name: "lstk-deploy", mode: 0o755, link: bundledBinaryBaseName},
			})
			dest := t.TempDir()
			if format == "zip" {
				require.NoError(t, extractZip(archive, dest))
			} else {
				require.NoError(t, extractTarGz(archive, dest))
			}
			requireAbsent(t, filepath.Join(dest, "lstk-deploy"))
			requireFileContent(t, filepath.Join(dest, bundledBinaryBaseName), "bundle")
		})
	}
}
