package update

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/localstack/lstk/internal/extension"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/version"
)

// missingBundledMembers reports which members of the bundled set this release
// ships are absent from the install directory, so `lstk update` can repair an
// incomplete install instead of short-circuiting on the version alone.
//
// It exists for one specific situation. A user crossing the transition on the
// binary channel is updated by their *old* lstk, which installs only the lstk
// binary and ignores the archive's other members, leaving a current binary
// with no bundled extensions. They cannot fix that by updating again, because
// applyUpdate always jumps straight to the newest release: they are already on
// it, so a version comparison reports "already up to date" until another
// release ships, potentially a week later.
//
// The directory probed is extension.BundledDir, by definition the directory
// the resolver reads bundled extensions from, so the completeness check and
// resolution can never look in different places.
//
// It returns nil, leaving the version comparison as the sole trigger exactly
// as before, in two cases that matter:
//
//   - The release ships no bundle (version.BundledSet is empty). Every
//     pre-bundling release, and any rollback to one, is therefore unaffected.
//   - lstk was installed by Homebrew or npm, where the package manager replaces
//     the whole package and so owns set completeness itself. Repairing there
//     would mean shelling out to the package manager for a version it has
//     already installed.
func missingBundledMembers() []string {
	expected := version.BundledSet()
	if len(expected) == 0 {
		return nil
	}
	if DetectInstallMethod().Method != InstallBinary {
		return nil
	}
	dir := extension.BundledDir(log.Nop())
	if dir == "" {
		return nil
	}
	return missingSetMembers(dir, expected)
}

// missingSetMembers returns the expected names that are absent from dir, in
// the order they were expected. "Absent" means missing or unusable: a name
// that stats but cannot run (a directory, or a binary without its exec bit)
// leaves the user exactly as stranded as no file at all, so it triggers the
// same repair. A stat failure that is not absence (a permission or I/O error)
// counts as present instead, because treating it as missing would re-download
// the whole release on every run over a transient error.
func missingSetMembers(dir string, expected []string) []string {
	var missing []string
	for _, name := range expected {
		info, err := os.Stat(filepath.Join(dir, name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, name)
		case err != nil:
			continue
		case !usableSetMember(info, name):
			missing = append(missing, name)
		}
	}
	return missing
}

// usableSetMember mirrors what extension resolution will accept (the
// resolver's side of the rule is isExecutableFile in internal/extension): a
// regular file, with an exec bit on Unix unless it is the descriptions file.
// On Windows executability is carried by the name, which the stamped set
// already includes.
func usableSetMember(info os.FileInfo, name string) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	if name == descriptionsFileName || goruntime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}
