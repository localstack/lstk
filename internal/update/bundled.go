package update

import (
	"os"
	"path/filepath"

	"github.com/localstack/lstk/internal/version"
)

// missingBundledMembers reports which members of the bundled set this release
// ships are absent from the install directory, so `lstk update` can repair an
// incomplete install instead of short-circuiting on the version alone.
//
// It exists for one specific situation. A user crossing the transition on the
// binary channel is updated by their *old* lstk, which installs only the lstk
// binary and ignores the archive's other members — leaving a current binary
// with no bundled extensions. They cannot fix that by updating again, because
// applyUpdate always jumps straight to the newest release: they are already on
// it, so a version comparison reports "already up to date" until another
// release ships, potentially a week later.
//
// It returns nil — leaving the version comparison as the sole trigger, exactly
// as before — in two cases that matter:
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
	info := DetectInstallMethod()
	if info.Method != InstallBinary || info.ResolvedPath == "" {
		return nil
	}
	return missingSetMembers(filepath.Dir(info.ResolvedPath), expected)
}

// missingSetMembers returns the expected names that are absent from dir, in the
// order they were expected.
func missingSetMembers(dir string, expected []string) []string {
	var missing []string
	for _, name := range expected {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}
