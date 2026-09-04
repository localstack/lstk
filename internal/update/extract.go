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

// stagingSuffix marks a member copied into the install directory but not yet
// renamed over its final name. Staging in the destination directory makes every
// commit an intra-directory rename: atomic, and never cross-device.
const stagingSuffix = ".lstk-new"

const descriptionsFileName = extension.DescriptionsFileName

// bundledBinaryBaseName is the multi-call binary providing every bundled
// extension; it is the one set member that does not match "lstk-*".
// TODO(dpx-692): alias from extension.BundledBinaryName once that branch lands.
const bundledBinaryBaseName = "bundled-extensions"

func exeName(base, goos string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

func bundledBinaryName(goos string) string { return exeName(bundledBinaryBaseName, goos) }

// updateMember is one file of the set an update installs.
type updateMember struct {
	src  string      // path inside the extracted archive
	dest string      // final path in the install directory
	mode os.FileMode // mode to install with
}

func (m updateMember) staging() string { return m.dest + stagingSuffix }

// commit renames the staged copy over the final name. On Windows a running
// executable can be renamed but not replaced, so an existing member is moved to
// ".old" first (lstk.exe itself, or a bundled extension the user is running);
// the ".old" is removed by the next update's commit.
func (m updateMember) commit(goos string) error {
	movedAside := ""
	if goos == "windows" {
		oldPath := m.dest + ".old"
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove old binary %s: %w", oldPath, err)
		}
		switch err := os.Rename(m.dest, oldPath); {
		case err == nil:
			movedAside = oldPath
		case os.IsNotExist(err):
		default:
			return fmt.Errorf("cannot move %s aside: %w", filepath.Base(m.dest), err)
		}
	}
	if err := os.Rename(m.staging(), m.dest); err != nil {
		if movedAside != "" {
			if rerr := os.Rename(movedAside, m.dest); rerr != nil {
				return fmt.Errorf("%w (restoring the previous file also failed: %v; rename %s back to %s by hand)",
					err, rerr, movedAside, m.dest)
			}
		}
		return err
	}
	return nil
}

// extractAndReplace installs every member the archive carries as one unit:
// lstk, the bundled-extensions binary, any lstk-* binaries, and the
// descriptions file. An archive carrying only lstk is a set of size one. A
// member that fails to stage or commit fails the whole update, naming it.
func extractAndReplace(archivePath, exePath, format string) error {
	return replaceSet(archivePath, exePath, format, goruntime.GOOS)
}

// replaceSet takes the platform as a parameter so the Windows naming and
// move-aside rules are testable on any host (unit tests run on Linux in CI).
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
	if err := removeStagingFiles(filepath.Dir(exePath)); err != nil {
		return err
	}
	if err := stageMembers(members); err != nil {
		return err
	}
	return commitMembers(members, goos)
}

// discoverMembers lists the set at the extracted archive root, lstk last: a
// failure before that final rename leaves a working lstk to re-run with.
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
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return nil, err
	}

	destDir := filepath.Dir(exePath)
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
		mode := os.FileMode(0o755)
		switch {
		case name == bundledBinaryName(goos):
		case name == descriptionsFileName:
			mode = 0o644
		case isExtensionEntry(name, info, goos):
		default:
			continue
		}
		members = append(members, updateMember{
			src:  filepath.Join(extractDir, name),
			dest: filepath.Join(destDir, name),
			mode: mode,
		})
	}
	// Destination and mode come from the running binary: the user may have
	// installed it under another name or with special bits (setgid), and the
	// pre-bundling updater preserved both.
	return append(members, updateMember{src: newBinary, dest: exePath, mode: exeInfo.Mode()}), nil
}

// isExtensionEntry accepts executable "lstk-*" regular files. On Windows only
// ".exe" counts: an installer must not let the user's PATHEXT decide what an
// archive installs, unlike the resolver (extension.scanDir), which honours it.
func isExtensionEntry(name string, info os.FileInfo, goos string) bool {
	if !strings.HasPrefix(name, extension.NamePrefix) || !info.Mode().IsRegular() {
		return false
	}
	if goos == "windows" {
		return strings.EqualFold(filepath.Ext(name), ".exe")
	}
	return info.Mode().Perm()&0o111 != 0
}

// removeStagingFiles deletes regular ".lstk-new" files left by an interrupted
// update. Matching is by literal suffix, not a glob: the path is user data and
// may contain glob metacharacters. Non-regular files are left for stageMembers
// to refuse.
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

// stageMembers copies every member to its staging name; on failure it removes
// what it staged and leaves the installation untouched. Anything already at a
// staging path is refused: a regular file means another update is running, and
// writing through a symlink or directory would damage the user's files.
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
				return fmt.Errorf("cannot stage %s: %s already exists; is another lstk update running?", filepath.Base(m.dest), path)
			}
			return fmt.Errorf("cannot stage %s: %s exists and is not a regular file; move it out of the way and re-run lstk update", filepath.Base(m.dest), path)
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

// commitMembers renames each staged file into place, stopping at the first
// failure. Uncommitted staging files are left for the next run to clean up.
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

// The extractors skip symlink entries: release archives ship none, and a zip
// symlink extracted as a file would be an "executable" holding a path string.
// Modes are restored with Chmod because OpenFile's mode is umask-masked and
// discoverMembers keys extension discovery off the exec bits.

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
		if err := os.Chmod(target, f.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src to a new file at dst (O_EXCL: never overwrites, never
// follows a symlink), syncs, and applies mode with Chmod so umask and the
// special bits are handled. A close error is reported: a full disk shows up
// there.
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
