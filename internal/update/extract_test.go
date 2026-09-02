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

// archiveFormats are the two release archive formats the updater accepts.
// Every set-replacement behavior is asserted against both, because goreleaser
// ships zip on Windows and tar.gz everywhere else and the two extractors are
// separate code paths.
var archiveFormats = []string{"tar.gz", "zip"}

type archiveEntry struct {
	name string
	body string
	mode os.FileMode
	// link, when set, makes this a symlink entry pointing at the given target
	// instead of a regular file. Release archives never contain one; it exists
	// to build the malformed archives the extractors must reject.
	link string
}

// lstkBinaryName is the archive-root name of the lstk binary on this platform.
func lstkBinaryName() string {
	if goruntime.GOOS == "windows" {
		return "lstk.exe"
	}
	return "lstk"
}

// extBinaryName is the archive-root name of the bundled extension providing
// the given command on this platform.
func extBinaryName(name string) string {
	if goruntime.GOOS == "windows" {
		return "lstk-" + name + ".exe"
	}
	return "lstk-" + name
}

// buildArchive writes a release-shaped archive containing exactly the given
// entries at its root and returns its path.
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
				// A zip symlink is a mode-flagged entry whose body is the target.
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
			require.NoError(t, tw.WriteHeader(&tar.Header{
				Name:     e.name,
				Mode:     int64(e.mode),
				Linkname: e.link,
				Typeflag: tar.TypeSymlink,
			}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Mode:     int64(e.mode),
			Size:     int64(len(e.body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(e.body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return path
}

// newInstallDir returns a fresh install directory containing the given files
// plus the path the running lstk binary would occupy.
func newInstallDir(t *testing.T, files map[string]string) (dir, exePath string) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		mode := os.FileMode(0o644)
		if name != descriptionsFileName {
			mode = 0o755
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
	if goruntime.GOOS == "windows" {
		// Windows has no execute bit; executability is decided by the .exe
		// suffix, which the archive-root name already carries.
		return
	}
	assert.NotZero(t, info.Mode().Perm()&0o111, "%s should be executable, got %v", path, info.Mode().Perm())
}

// requireNoStagingLeftovers asserts the install directory holds no staging
// files, which is what makes a later re-run a clean repair rather than a
// resume of unknown state.
func requireNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+stagingSuffix))
	require.NoError(t, err)
	var files []string
	for _, m := range matches {
		info, err := os.Stat(m)
		require.NoError(t, err)
		if info.Mode().IsRegular() {
			files = append(files, m)
		}
	}
	assert.Empty(t, files, "no staging files should be left behind")
}

// TestExtractAndReplaceReplacesWholeSet covers the core promise of the
// set-wise updater: lstk, every bundled extension, and the descriptions file
// are replaced together by one update.
func TestExtractAndReplaceReplacesWholeSet(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			dir, exePath := newInstallDir(t, map[string]string{
				lstkBinaryName():       "old lstk",
				extBinaryName("alpha"): "old alpha",
				extBinaryName("beta"):  "old beta",
				descriptionsFileName:   "alpha = \"old alpha\"\n",
			})

			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
				{name: extBinaryName("beta"), body: "new beta", mode: 0o755},
				{name: descriptionsFileName, body: "alpha = \"new alpha\"\nbeta = \"new beta\"\n", mode: 0o644},
			})

			require.NoError(t, extractAndReplace(archive, exePath, format))

			requireFileContent(t, exePath, "new lstk")
			requireFileContent(t, filepath.Join(dir, extBinaryName("alpha")), "new alpha")
			requireFileContent(t, filepath.Join(dir, extBinaryName("beta")), "new beta")
			requireFileContent(t, filepath.Join(dir, descriptionsFileName), "alpha = \"new alpha\"\nbeta = \"new beta\"\n")
			requireExecutable(t, exePath)
			requireExecutable(t, filepath.Join(dir, extBinaryName("alpha")))
			requireExecutable(t, filepath.Join(dir, extBinaryName("beta")))
			requireNoStagingLeftovers(t, dir)
		})
	}
}

// TestExtractAndReplaceInstallsNewExtension covers a release that adds an
// extension the install did not have.
func TestExtractAndReplaceInstallsNewExtension(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			dir, exePath := newInstallDir(t, map[string]string{
				lstkBinaryName(): "old lstk",
			})

			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: extBinaryName("deploy"), body: "new deploy", mode: 0o755},
				{name: descriptionsFileName, body: "deploy = \"Deploy\"\n", mode: 0o644},
			})

			require.NoError(t, extractAndReplace(archive, exePath, format))

			requireFileContent(t, exePath, "new lstk")
			requireFileContent(t, filepath.Join(dir, extBinaryName("deploy")), "new deploy")
			requireExecutable(t, filepath.Join(dir, extBinaryName("deploy")))
			requireFileContent(t, filepath.Join(dir, descriptionsFileName), "deploy = \"Deploy\"\n")
			requireNoStagingLeftovers(t, dir)
		})
	}
}

// TestExtractAndReplaceLstkOnlyArchive pins the pre-bundling and rollback
// shape: an archive carrying only lstk is a valid set of size one and must
// behave exactly as the updater did before bundling existed.
func TestExtractAndReplaceLstkOnlyArchive(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			dir, exePath := newInstallDir(t, map[string]string{
				lstkBinaryName():       "old lstk",
				extBinaryName("alpha"): "installed alpha",
			})

			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
			})

			require.NoError(t, extractAndReplace(archive, exePath, format))

			requireFileContent(t, exePath, "new lstk")
			// Rollback leaves previously installed extensions in place and runnable.
			requireFileContent(t, filepath.Join(dir, extBinaryName("alpha")), "installed alpha")
			requireExecutable(t, filepath.Join(dir, extBinaryName("alpha")))
			requireNoStagingLeftovers(t, dir)
		})
	}
}

// TestExtractAndReplaceLeavesUnmatchedExtensionAlone pins the additive-only
// rule: the updater cannot tell a dropped bundled extension from one the user
// placed there, so it never deletes an lstk-* file absent from the archive.
func TestExtractAndReplaceLeavesUnmatchedExtensionAlone(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName():      "old lstk",
		extBinaryName("mine"): "user extension",
	})

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("deploy"), body: "new deploy", mode: 0o755},
		{name: descriptionsFileName, body: "deploy = \"Deploy\"\n", mode: 0o644},
	})

	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))

	requireFileContent(t, filepath.Join(dir, extBinaryName("mine")), "user extension")
	requireFileContent(t, filepath.Join(dir, extBinaryName("deploy")), "new deploy")
}

// TestExtractAndReplaceStagingFailureLeavesInstallUntouched proves a failure
// before commit cannot produce a partial set: nothing under a final name
// changes, and no staging files survive.
func TestExtractAndReplaceStagingFailureLeavesInstallUntouched(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName():       "old lstk",
		extBinaryName("alpha"): "old alpha",
		extBinaryName("beta"):  "old beta",
		descriptionsFileName:   "alpha = \"old\"\n",
	})

	// A non-empty directory squatting on beta's staging path makes its copy
	// fail after alpha has already been staged — a failure partway through.
	blocked := filepath.Join(dir, extBinaryName("beta")+stagingSuffix)
	require.NoError(t, os.MkdirAll(blocked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o644))

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
		{name: extBinaryName("beta"), body: "new beta", mode: 0o755},
		{name: descriptionsFileName, body: "alpha = \"new\"\n", mode: 0o644},
	})

	err := extractAndReplace(archive, exePath, "tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), extBinaryName("beta"), "error should name the member that failed")

	requireFileContent(t, exePath, "old lstk")
	requireFileContent(t, filepath.Join(dir, extBinaryName("alpha")), "old alpha")
	requireFileContent(t, filepath.Join(dir, extBinaryName("beta")), "old beta")
	requireFileContent(t, filepath.Join(dir, descriptionsFileName), "alpha = \"old\"\n")
	requireNoStagingLeftovers(t, dir)
}

// TestExtractAndReplaceCleansLeftoverStagingFiles proves that re-running
// `lstk update` repairs an update that crashed partway through: leftover
// staging files from the crashed run are removed before the new one stages.
func TestExtractAndReplaceCleansLeftoverStagingFiles(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName(): "old lstk",
	})

	// Leftovers a crashed update would have left behind, including one for a
	// member the new archive does not carry.
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
	requireNoStagingLeftovers(t, dir)
	// The stale leftover must not have been committed under a real name.
	_, err := os.Stat(filepath.Join(dir, extBinaryName("gone")))
	assert.True(t, os.IsNotExist(err), "a leftover staging file must never become a real member")
}

// TestExtractAndReplaceCommitFailureKeepsPreviousLstk is the case that
// justifies committing lstk last: when a member fails to commit, the user is
// left on their previous, complete version rather than a new lstk with an
// incomplete set.
func TestExtractAndReplaceCommitFailureKeepsPreviousLstk(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName(): "old lstk",
	})

	// A non-empty directory under alpha's final name makes its commit rename
	// fail, after staging has fully succeeded.
	blocked := filepath.Join(dir, extBinaryName("alpha"))
	require.NoError(t, os.MkdirAll(blocked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "occupied"), []byte("x"), 0o644))

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
		{name: descriptionsFileName, body: "alpha = \"new\"\n", mode: 0o644},
	})

	err := extractAndReplace(archive, exePath, "tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), extBinaryName("alpha"), "error should name the member that failed")

	// lstk must still be the previous version.
	requireFileContent(t, exePath, "old lstk")
}

// TestExtractAndReplaceMissingBinary keeps the pre-existing contract that an
// archive without the lstk binary is rejected.
func TestExtractAndReplaceMissingBinary(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName(): "old lstk",
	})

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: extBinaryName("alpha"), body: "new alpha", mode: 0o755},
	})

	err := extractAndReplace(archive, exePath, "tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found in archive")
	requireFileContent(t, exePath, "old lstk")
	requireNoStagingLeftovers(t, dir)
}

// TestExtractAndReplaceIgnoresNonExecutableArchiveEntries proves the discovery
// rule does not mistake a data file shipped next to the binaries for an
// extension.
func TestExtractAndReplaceIgnoresNonExecutableArchiveEntries(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName(): "old lstk",
	})

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: "lstk-notes.txt", body: "not an extension", mode: 0o644},
		{name: "README.md", body: "readme", mode: 0o644},
		{name: "LICENSE", body: "license", mode: 0o644},
	})

	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))

	requireFileContent(t, exePath, "new lstk")
	for _, name := range []string{"lstk-notes.txt", "README.md", "LICENSE"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(err), "%s must not be installed", name)
	}
}

// TestReplaceSetWindowsVariant exercises the Windows shape of an update —
// zip archive, ".exe" names, and moving the running lstk.exe aside before
// replacing it — from any host. Unit tests run on Linux only in CI, so without
// the goos parameter this path would ship untested.
func TestReplaceSetWindowsVariant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "lstk.exe")
	require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-alpha.exe"), []byte("old alpha"), 0o755))

	archive := buildArchive(t, "zip", []archiveEntry{
		{name: "lstk.exe", body: "new lstk", mode: 0o755},
		{name: "lstk-alpha.exe", body: "new alpha", mode: 0o755},
		{name: "lstk-beta.exe", body: "new beta", mode: 0o755},
		// Neither of these is an extension on Windows: executability there is
		// decided by the suffix, not by a mode bit the archive cannot carry.
		{name: "lstk-notes.txt", body: "not an extension", mode: 0o644},
		{name: "lstk-plain", body: "not an extension either", mode: 0o755},
		{name: descriptionsFileName, body: "alpha = \"Alpha\"\nbeta = \"Beta\"\n", mode: 0o644},
	})

	require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))

	requireFileContent(t, exePath, "new lstk")
	requireFileContent(t, filepath.Join(dir, "lstk-alpha.exe"), "new alpha")
	requireFileContent(t, filepath.Join(dir, "lstk-beta.exe"), "new beta")
	requireFileContent(t, filepath.Join(dir, descriptionsFileName), "alpha = \"Alpha\"\nbeta = \"Beta\"\n")
	// The running binary is renamed aside rather than overwritten.
	requireFileContent(t, filepath.Join(dir, "lstk.exe.old"), "old lstk")
	for _, name := range []string{"lstk-notes.txt", "lstk-plain"} {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(err), "%s must not be installed as an extension", name)
	}
	requireNoStagingLeftovers(t, dir)
}

// TestCommitRestoresRunningBinaryOnWindowsRenameFailure covers the one gap in
// "re-run lstk update to repair" on Windows: the running lstk.exe is moved
// aside before the final rename, so a failure there would leave nothing under
// the real name — and no lstk to re-run. The commit must move it back.
func TestCommitRestoresRunningBinaryOnWindowsRenameFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dest := filepath.Join(dir, "lstk.exe")
	require.NoError(t, os.WriteFile(dest, []byte("old lstk"), 0o755))

	// No staging file exists, so the final rename fails after the aside-move.
	m := updateMember{dest: dest, mode: 0o755, running: true}
	require.Error(t, m.commit("windows"))

	requireFileContent(t, dest, "old lstk")
	_, err := os.Stat(dest + ".old")
	assert.True(t, os.IsNotExist(err), "the moved-aside binary should have been moved back")
}

// TestReplaceSetWindowsVariantLstkOnlyArchive pins the rollback shape on the
// Windows path, including the second run's cleanup of the leftover .old file.
func TestReplaceSetWindowsVariantLstkOnlyArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "lstk.exe")
	require.NoError(t, os.WriteFile(exePath, []byte("old lstk"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk.exe.old"), []byte("older lstk"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lstk-alpha.exe"), []byte("installed alpha"), 0o755))

	archive := buildArchive(t, "zip", []archiveEntry{{name: "lstk.exe", body: "new lstk", mode: 0o755}})

	require.NoError(t, replaceSet(archive, exePath, "zip", "windows"))

	requireFileContent(t, exePath, "new lstk")
	requireFileContent(t, filepath.Join(dir, "lstk.exe.old"), "old lstk")
	requireFileContent(t, filepath.Join(dir, "lstk-alpha.exe"), "installed alpha")
	requireNoStagingLeftovers(t, dir)
}

// TestExtractAndReplaceInstallsBundle covers the shape a bundling release
// actually ships: the single multi-call binary and the descriptions file, both
// installed as ordinary members of the set.
func TestExtractAndReplaceInstallsBundle(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			dir, exePath := newInstallDir(t, map[string]string{
				lstkBinaryName():      "old lstk",
				bundledBinaryBaseName: "old multi-call binary",
				descriptionsFileName:  "deploy = \"Deploy\"\n",
			})

			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryBaseName, body: "new multi-call binary", mode: 0o755},
				{name: descriptionsFileName, body: "deploy = \"Deploy\"\ndoctor = \"Doctor\"\n", mode: 0o644},
			})

			require.NoError(t, extractAndReplace(archive, exePath, format))

			requireFileContent(t, exePath, "new lstk")
			requireFileContent(t, filepath.Join(dir, bundledBinaryBaseName), "new multi-call binary")
			requireExecutable(t, filepath.Join(dir, bundledBinaryBaseName))
			requireFileContent(t, filepath.Join(dir, descriptionsFileName), "deploy = \"Deploy\"\ndoctor = \"Doctor\"\n")
			requireNoStagingLeftovers(t, dir)
		})
	}
}

// TestExtractAndReplaceInstallsBundleWithoutDescriptions covers a bundle whose
// descriptions file is absent: the binary still installs and the update
// succeeds, since the descriptions file only affects help rendering.
func TestExtractAndReplaceInstallsBundleWithoutDescriptions(t *testing.T) {
	t.Parallel()
	dir, exePath := newInstallDir(t, map[string]string{
		lstkBinaryName(): "old lstk",
	})

	archive := buildArchive(t, "tar.gz", []archiveEntry{
		{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
		{name: bundledBinaryBaseName, body: "multi-call binary", mode: 0o755},
	})

	require.NoError(t, extractAndReplace(archive, exePath, "tar.gz"))

	requireFileContent(t, exePath, "new lstk")
	requireFileContent(t, filepath.Join(dir, bundledBinaryBaseName), "multi-call binary")
	requireNoStagingLeftovers(t, dir)
}

// TestExtractorsSkipSymlinkEntries keeps a malformed or hostile archive from
// installing junk. No release archive ships links, but a zip symlink entry
// stores its target as the file body, so extracting one as a regular file
// yields an executable whose contents are a path string — which discoverMembers
// would then install as a bundled extension.
func TestExtractorsSkipSymlinkEntries(t *testing.T) {
	t.Parallel()
	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			archive := buildArchive(t, format, []archiveEntry{
				{name: lstkBinaryName(), body: "new lstk", mode: 0o755},
				{name: bundledBinaryBaseName, body: "multi-call binary", mode: 0o755},
				{name: "lstk-deploy", mode: 0o755, link: bundledBinaryBaseName},
			})

			dest := t.TempDir()
			if format == "zip" {
				require.NoError(t, extractZip(archive, dest))
			} else {
				require.NoError(t, extractTarGz(archive, dest))
			}

			_, err := os.Lstat(filepath.Join(dest, "lstk-deploy"))
			assert.True(t, os.IsNotExist(err), "an archive symlink entry must not be materialized")
			requireFileContent(t, filepath.Join(dest, bundledBinaryBaseName), "multi-call binary")
			requireFileContent(t, filepath.Join(dest, lstkBinaryName()), "new lstk")
		})
	}
}
