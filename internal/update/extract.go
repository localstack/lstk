package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/localstack/lstk/internal/extension"
)

// stagingSuffix marks a set member that has been copied into the install
// directory but not yet committed under its final name. Staging inside the
// destination directory (rather than in the extraction temp dir) is what makes
// every commit an intra-directory rename: same filesystem, so it is atomic and
// cannot fail with EXDEV. The cross-device case that used to need a copy
// fallback is now handled by construction, because the only cross-filesystem
// hop — temp dir to install dir — is always a copy.
const stagingSuffix = ".lstk-new"

// descriptionsFileName is the bundled descriptions file an archive ships
// alongside the extension binaries. It is aliased from the extension package so
// the updater and the runtime resolver can never disagree on its name.
const descriptionsFileName = extension.DescriptionsFileName

// bundledBinaryBaseName is the single multi-call binary that provides every
// bundled extension: one program that dispatches on the command it is asked
// for, rather than one binary per extension. It must be discovered by exact
// name, because it is the one member of the set that does not match "lstk-*".
const bundledBinaryBaseName = "bundled-extensions"

// bundledBinaryName is the archive-root name of the multi-call binary on the
// given platform.
func bundledBinaryName(goos string) string {
	if goos == "windows" {
		return bundledBinaryBaseName + ".exe"
	}
	return bundledBinaryBaseName
}

// updateMember is one file of the version-matched set an update installs: the
// lstk binary, a bundled extension binary, or the descriptions file.
type updateMember struct {
	src     string      // path inside the extracted archive
	dest    string      // final path in the install directory
	mode    os.FileMode // permissions to install with
	running bool        // true for the lstk binary executing this update
}

// staging is the temporary sibling this member is copied to before commit.
func (m updateMember) staging() string { return m.dest + stagingSuffix }

// commit renames the staged copy over the member's final name.
func (m updateMember) commit(goos string) error {
	// On Windows a running executable cannot be overwritten but can be
	// renamed. Move it out of the way first so the new binary can take the
	// original path. This applies to lstk itself only: bundled extensions are
	// not running during an update.
	movedAside := ""
	if m.running && goos == "windows" {
		oldPath := m.dest + ".old"
		// Clean up a leftover from a previous update; ignore if absent.
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove old binary %s: %w", oldPath, err)
		}
		if err := os.Rename(m.dest, oldPath); err != nil {
			return fmt.Errorf("cannot move running binary: %w", err)
		}
		movedAside = oldPath
	}
	if err := os.Rename(m.staging(), m.dest); err != nil {
		if movedAside != "" {
			// The running binary was already moved aside, so failing here would
			// leave nothing under the real name — and no lstk left to run, so
			// "re-run lstk update" could not repair it. Move it back so the
			// user stays on their previous, complete version.
			_ = os.Rename(movedAside, m.dest)
		}
		return err
	}
	return nil
}

// extractAndReplace extracts the downloaded release archive and installs every
// member of the set it carries — the lstk binary, the multi-call
// "bundled-extensions" binary, any "lstk-*" extension binaries, and the
// descriptions file — as one unit, using stage-then-commit.
//
// The set is whatever the archive contains: an archive carrying only lstk (a
// pre-bundling release, or a rollback to one) is a valid set of size one and is
// installed exactly as the pre-bundling updater did. When an archive does carry
// extensions they are not optional — any member that fails to stage or commit
// fails the whole update, naming the member, rather than reporting success with
// a partial set.
func extractAndReplace(archivePath, exePath, format string) error {
	return replaceSet(archivePath, exePath, format, goruntime.GOOS)
}

// replaceSet is extractAndReplace with the target platform as a parameter, so
// the Windows naming rules and the move-the-running-exe-aside commit can be
// exercised from a test on any host. Unit tests run on Linux only in CI, and
// this is the update path — the one thing a bad release cannot ship a fix for.
func replaceSet(archivePath, exePath, format, goos string) error {
	dir, err := os.MkdirTemp("", "lstk-extract-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	switch format {
	case "tar.gz":
		if err := extractTarGz(archivePath, dir); err != nil {
			return fmt.Errorf("extract failed: %w", err)
		}
	case "zip":
		if err := extractZip(archivePath, dir); err != nil {
			return fmt.Errorf("extract failed: %w", err)
		}
	}

	members, err := discoverMembers(dir, exePath, goos)
	if err != nil {
		return err
	}

	// A previous update that died between staging and commit leaves staging
	// files behind. Removing them first is what makes re-running `lstk update`
	// a clean repair instead of a resume of unknown state.
	if err := removeStagingFiles(filepath.Dir(exePath)); err != nil {
		return err
	}

	if err := stageMembers(members); err != nil {
		return err
	}
	return commitMembers(members, goos)
}

// discoverMembers builds the set to install from the extracted archive root:
// the multi-call bundled binary, every executable "lstk-*" file, the
// descriptions file, and the lstk binary. The "lstk-*" case covers an archive
// that ships extensions as standalone binaries; a bundle built the current way
// contributes none, because it ships one multi-call binary instead.
//
// The returned order is the commit order — extensions and the descriptions file
// first, the lstk binary last. Committing lstk last is what keeps a failed
// commit safe: any failure before that final rename leaves the user on their
// previous, complete version.
func discoverMembers(extractDir, exePath, goos string) ([]updateMember, error) {
	binaryName := "lstk"
	if goos == "windows" {
		binaryName = "lstk.exe"
	}

	newBinary := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(newBinary); err != nil {
		return nil, fmt.Errorf("binary not found in archive: %w", err)
	}

	exeInfo, err := os.Stat(exePath)
	if err != nil {
		return nil, err
	}

	destDir := filepath.Dir(exePath)
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return nil, err
	}

	// os.ReadDir sorts by filename, so the member order is deterministic.
	var members []updateMember
	var descriptions []updateMember
	for _, entry := range entries {
		name := entry.Name()
		if name == binaryName {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		switch {
		case name == bundledBinaryName(goos):
			members = append(members, updateMember{
				src:  filepath.Join(extractDir, name),
				dest: filepath.Join(destDir, name),
				mode: 0o755,
			})
		case name == descriptionsFileName:
			descriptions = append(descriptions, updateMember{
				src:  filepath.Join(extractDir, name),
				dest: filepath.Join(destDir, name),
				mode: 0o644,
			})
		case isExtensionEntry(name, info, goos):
			members = append(members, updateMember{
				src:  filepath.Join(extractDir, name),
				dest: filepath.Join(destDir, name),
				mode: 0o755,
			})
		}
	}

	members = append(members, descriptions...)
	// The destination is exePath rather than destDir/binaryName: the user may
	// have installed the binary under a different name, and the pre-bundling
	// updater replaced whatever it was running as. Its mode is preserved for
	// the same reason.
	return append(members, updateMember{
		src:     newBinary,
		dest:    exePath,
		mode:    exeInfo.Mode().Perm(),
		running: true,
	}), nil
}

// isExtensionEntry reports whether an archive-root entry is a bundled
// extension binary. The descriptions file is excluded by the caller before this
// runs, since it shares the "lstk-" prefix.
func isExtensionEntry(name string, info os.FileInfo, goos string) bool {
	if !strings.HasPrefix(name, extension.NamePrefix) || !info.Mode().IsRegular() {
		return false
	}
	if goos == "windows" {
		// Windows archives carry no execute bit, so the ".exe" suffix — which
		// is how the release names bundled extension binaries, and what
		// resolution looks for — is what identifies one. Without this check
		// any lstk-*.txt shipped at the archive root would install as an
		// extension.
		return strings.EqualFold(filepath.Ext(name), ".exe")
	}
	return info.Mode().Perm()&0o111 != 0
}

// removeStagingFiles deletes staging files left in dir by an interrupted
// update. Only regular files are removed: the updater only ever creates regular
// files there, so anything else carrying the suffix belongs to the user and
// must not be deleted.
func removeStagingFiles(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+stagingSuffix))
	if err != nil {
		return err
	}
	for _, path := range matches {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("cannot remove leftover staging file %s: %w", path, err)
		}
	}
	return nil
}

// stageMembers copies every member next to its destination under the staging
// name. A failure removes what was staged so far and leaves the installation
// completely untouched — nothing has been committed at this point.
func stageMembers(members []updateMember) error {
	staged := make([]string, 0, len(members))
	unstage := func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}

	for _, m := range members {
		path := m.staging()
		if err := copyFile(m.src, path, m.mode); err != nil {
			_ = os.Remove(path)
			unstage()
			return fmt.Errorf("cannot stage %s: %w", filepath.Base(m.dest), err)
		}
		staged = append(staged, path)
		// copyFile's mode only applies on creation and is masked by umask, so
		// binaries get their execute bits set explicitly.
		if err := os.Chmod(path, m.mode); err != nil {
			unstage()
			return fmt.Errorf("cannot set permissions on %s: %w", filepath.Base(m.dest), err)
		}
	}
	return nil
}

// commitMembers renames each staged file over its final name, in the order
// discoverMembers returned (lstk last). It stops at the first failure and names
// the member that failed; staging files for members not yet committed are left
// for the next run to clean up, since removing them could fail for the same
// reason the rename did and mask the real error.
func commitMembers(members []updateMember, goos string) error {
	for _, m := range members {
		if err := m.commit(goos); err != nil {
			return fmt.Errorf("cannot install %s: %w", filepath.Base(m.dest), err)
		}
	}
	return nil
}

func safePath(destDir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("archive contains absolute path: %s", name)
	}
	target := filepath.Join(destDir, filepath.Clean(name))
	rel, err := filepath.Rel(destDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}
	return target, nil
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target, err := safePath(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			// Skipped deliberately, not by omission. No release archive ships
			// links, and one appearing here would mean a malformed or hostile
			// archive — see extractZip for the concrete hazard.
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
			// OpenFile's mode is masked by the process umask, and
			// discoverMembers keys extension discovery off the extracted exec
			// bits; Chmod is not masked, so it restores the archive's mode.
			if err := os.Chmod(target, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target, err := safePath(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		// A zip symlink entry stores its target as the file body, so writing it
		// out as a regular file produces an executable whose contents are a
		// path string — which discoverMembers would then install as an
		// extension. No release archive ships links, so skipping is free; the
		// check exists so a malformed archive cannot install junk.
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return err
		}
		_ = out.Close()
		_ = rc.Close()
		// Same umask concern as extractTarGz: restore the archive's mode so
		// extension discovery sees the exec bits the release shipped.
		if err := os.Chmod(target, f.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src to dst with the given mode. It flushes to disk and
// reports a close failure as an error: a full disk surfaces only at flush or
// close, and a copy that silently half-succeeded would be committed over a
// working file.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
