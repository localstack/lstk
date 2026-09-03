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
// fallback is handled by construction, because the only cross-filesystem hop
// (temp dir to install dir) is always a copy.
const stagingSuffix = ".lstk-new"

// descriptionsFileName is the bundled descriptions file an archive ships
// alongside the extension binaries. It is aliased from the extension package so
// the updater and the runtime resolver can never disagree on its name.
const descriptionsFileName = extension.DescriptionsFileName

// bundledBinaryBaseName is the single multi-call binary that provides every
// bundled extension: one program that dispatches on the command it is asked
// for, rather than one binary per extension. It must be discovered by exact
// name, because it is the one member of the set that does not match "lstk-*".
//
// TODO(dpx-692): the extension-bundling branch exports this same name as
// extension.BundledBinaryName for the runtime resolver. Once that branch
// lands, alias this constant from there the way descriptionsFileName already
// is, so the updater and the resolver can never disagree on the name.
const bundledBinaryBaseName = "bundled-extensions"

// exeName returns base with the platform executable suffix appended on
// Windows. Every construction of a member file name goes through this, so the
// suffix rule cannot drift between discovery, staging and the tests.
func exeName(base, goos string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

// bundledBinaryName is the archive-root name of the multi-call binary on the
// given platform.
func bundledBinaryName(goos string) string {
	return exeName(bundledBinaryBaseName, goos)
}

// updateMember is one file of the version-matched set an update installs: the
// lstk binary, a bundled extension binary, or the descriptions file.
type updateMember struct {
	src  string      // path inside the extracted archive
	dest string      // final path in the install directory
	mode os.FileMode // permissions to install with
}

// staging is the temporary sibling this member is copied to before commit.
func (m updateMember) staging() string { return m.dest + stagingSuffix }

// commit renames the staged copy over the member's final name.
func (m updateMember) commit(goos string) error {
	// On Windows a running executable cannot be replaced but can be renamed,
	// so every existing member is moved aside first. That obviously covers the
	// lstk.exe executing this update, but also a bundled extension the user
	// happens to be running in another terminal while the update commits. The
	// ".old" file is left behind on success (the running lstk.exe cannot
	// delete itself) and is cleaned up by the next update's commit.
	movedAside := ""
	if goos == "windows" {
		oldPath := m.dest + ".old"
		// Clean up a leftover from a previous update; ignore if absent.
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove old binary %s: %w", oldPath, err)
		}
		switch err := os.Rename(m.dest, oldPath); {
		case err == nil:
			movedAside = oldPath
		case os.IsNotExist(err):
			// A member the install did not have yet; nothing to move.
		default:
			return fmt.Errorf("cannot move %s aside: %w", filepath.Base(m.dest), err)
		}
	}
	if err := os.Rename(m.staging(), m.dest); err != nil {
		if movedAside != "" {
			// The old file was already moved aside, so failing here would leave
			// nothing under the real name. For lstk itself that would mean no
			// binary left to re-run the update with. Move it back so the user
			// keeps the previous version.
			if rerr := os.Rename(movedAside, m.dest); rerr != nil {
				return fmt.Errorf("%w (restoring the previous file also failed: %v; rename %s back to %s by hand to recover)",
					err, rerr, movedAside, m.dest)
			}
		}
		return err
	}
	return nil
}

// extractAndReplace extracts the downloaded release archive and installs every
// member of the set it carries (the lstk binary, the multi-call
// "bundled-extensions" binary, any "lstk-*" extension binaries, and the
// descriptions file) as one unit, using stage-then-commit.
//
// The set is whatever the archive contains: an archive carrying only lstk (a
// pre-bundling release, or a rollback to one) is a valid set of size one and is
// installed exactly as the pre-bundling updater did. When an archive does carry
// extensions they are not optional: any member that fails to stage or commit
// fails the whole update, naming the member, rather than reporting success with
// a partial set.
func extractAndReplace(archivePath, exePath, format string) error {
	return replaceSet(archivePath, exePath, format, goruntime.GOOS)
}

// replaceSet is extractAndReplace with the target platform as a parameter, so
// the Windows naming rules and the move-aside commit can be exercised from a
// test on any host. Unit tests run on Linux only in CI, and this is the update
// path: the one thing a bad release cannot ship a fix for.
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
// The returned order is the commit order. The only ordering that matters is
// that the lstk binary goes last: committing lstk last is what keeps a failed
// commit safe, because any failure before that final rename leaves the user
// with a working lstk to re-run the update with.
func discoverMembers(extractDir, exePath, goos string) ([]updateMember, error) {
	binaryName := exeName("lstk", goos)

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
			members = append(members, updateMember{
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

	// The destination is exePath rather than destDir/binaryName: the user may
	// have installed the binary under a different name, and the pre-bundling
	// updater replaced whatever it was running as. The full mode is preserved
	// for the same reason, special bits included: an lstk installed setgid
	// must still be setgid after the update (os.Chmod applies the
	// setuid/setgid/sticky bits as well as the permissions).
	return append(members, updateMember{
		src:  newBinary,
		dest: exePath,
		mode: exeInfo.Mode(),
	}), nil
}

// isExtensionEntry reports whether an archive-root entry is a bundled
// extension binary. The descriptions file is excluded by the caller before this
// runs, since it shares the "lstk-" prefix.
//
// On Windows this is deliberately narrower than runtime resolution: the
// resolver accepts the whole PATHEXT set (see scanDir and windowsExts in
// internal/extension/resolve.go), while an installer must not let the user's
// PATHEXT decide what a release archive installs, so only ".exe" is accepted
// here. A release shipping any other launcher shape must widen both sides
// together.
func isExtensionEntry(name string, info os.FileInfo, goos string) bool {
	if !strings.HasPrefix(name, extension.NamePrefix) || !info.Mode().IsRegular() {
		return false
	}
	if goos == "windows" {
		// Windows archives carry no execute bit, so the ".exe" suffix (how the
		// release names bundled extension binaries, and what resolution looks
		// for) is what identifies one. Without this check any lstk-*.txt
		// shipped at the archive root would install as an extension.
		return strings.EqualFold(filepath.Ext(name), ".exe")
	}
	return info.Mode().Perm()&0o111 != 0
}

// removeStagingFiles deletes staging files left in dir by an interrupted
// update. Only regular files are removed: the updater only ever creates regular
// files there, so anything else carrying the suffix belongs to the user and
// must not be deleted (stageMembers then refuses to write through it).
//
// The directory is listed and matched by literal suffix, never by pattern:
// the install path is user data, and a glob would misread metacharacters in
// it (an unmatched "[" fails outright, a matched pair matches nothing and
// silently skips the cleanup).
func removeStagingFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), stagingSuffix) || !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("cannot remove leftover staging file %s: %w", path, err)
		}
	}
	return nil
}

// stageMembers copies every member next to its destination under the staging
// name. A failure removes what was staged so far and leaves the installation
// completely untouched, since nothing has been committed at this point.
//
// Anything already sitting at a staging path is refused rather than written
// over. The cleanup pass has just removed the updater's own leftovers, so a
// regular file appearing here means another update is running, and a
// non-regular one is the user's (writing through a symlink would destroy
// whatever it points at, and the commit would then install the symlink itself
// as the member). Refusing is also what keeps two concurrent updates from
// committing each other's half-written copies.
func stageMembers(members []updateMember) error {
	staged := make([]string, 0, len(members))
	unstage := func() {
		for _, path := range staged {
			_ = os.Remove(path)
		}
	}

	for _, m := range members {
		path := m.staging()
		if info, err := os.Lstat(path); err == nil {
			unstage()
			if info.Mode().IsRegular() {
				return fmt.Errorf("cannot stage %s: %s already exists; is another lstk update running?",
					filepath.Base(m.dest), path)
			}
			return fmt.Errorf("cannot stage %s: %s exists and is not a regular file; move it out of the way and re-run lstk update",
				filepath.Base(m.dest), path)
		}
		if err := copyFile(m.src, path, m.mode); err != nil {
			_ = os.Remove(path)
			unstage()
			return fmt.Errorf("cannot stage %s in %s: %w (the update needs write permission in this directory)",
				filepath.Base(m.dest), filepath.Dir(m.dest), err)
		}
		staged = append(staged, path)
	}
	return nil
}

// commitMembers renames each staged file over its final name, in the order
// discoverMembers returned (lstk last). It stops at the first failure and
// names the member that failed. Staging files for members not yet committed
// are left for the next run to clean up, since removing them could fail for
// the same reason the rename did and mask the real error. Members committed
// before the failure stay at the new version; that skew is benign by contract
// (the extension API version only changes on breaking releases) and is
// resolved by re-running the update.
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
			// archive; see extractZip for the concrete hazard.
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
		// path string, which discoverMembers would then install as an
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

// copyFile copies src to a NEW file at dst with the given mode, refusing to
// overwrite anything (O_EXCL, which also never follows a symlink). It flushes
// to disk and reports a close failure as an error: a full disk surfaces only
// at flush or close, and a copy that silently half-succeeded would be
// committed over a working file.
//
// The mode is applied with an explicit Chmod after the bytes are safely on
// disk, because the permission argument to OpenFile is masked by the process
// umask and drops the special bits; Chmod is subject to neither, so the file
// ends up with exactly the requested mode, setuid/setgid/sticky included.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
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
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}
