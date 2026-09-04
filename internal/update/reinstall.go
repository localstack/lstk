package update

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/localstack/lstk/internal/version"
)

// MissingBundle describes a bundling release whose bundled extensions are not
// installed beside lstk.
type MissingBundle struct {
	Dir       string // install directory that should hold the bundle
	Reinstall string // what restores the complete set for this install method
}

// Summary is the one-sentence explanation shown wherever the state is reported.
func (m MissingBundle) Summary() string {
	return fmt.Sprintf("This lstk release ships bundled extensions, but none are installed in %s.", m.Dir)
}

// DetectMissingBundle reports whether the running lstk was built to ship
// bundled extensions (version.BundlesExtensions) and has none beside it. Dev
// builds never report one.
func DetectMissingBundle() (MissingBundle, bool) {
	if !version.BundlesExtensions() {
		return MissingBundle{}, false
	}
	info := DetectInstallMethod()
	if info.ResolvedPath == "" {
		return MissingBundle{}, false
	}
	return detectMissingBundle(info, goruntime.GOOS)
}

func detectMissingBundle(info InstallInfo, goos string) (MissingBundle, bool) {
	dir := filepath.Dir(info.ResolvedPath)
	if !bundleMissing(dir, goos) {
		return MissingBundle{}, false
	}
	return MissingBundle{Dir: dir, Reinstall: ReinstallCommand(info.Method)}, true
}

// bundleMissing is true only when neither set member exists. A binary without
// its toml is a different, corrupt state, left to the extension resolver.
func bundleMissing(dir, goos string) bool {
	for _, name := range []string{bundledBinaryName(goos), descriptionsFileName} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			return false
		}
	}
	return true
}

// ReinstallCommand returns the command, or the download page for a plain
// binary, that installs the complete set for the given install method.
func ReinstallCommand(m InstallMethod) string {
	switch m {
	case InstallHomebrew:
		return "brew reinstall --cask localstack/tap/lstk"
	case InstallNPM:
		return "npm install -g @localstack/lstk"
	default:
		return "download the latest release from https://github.com/localstack/lstk/releases/latest"
	}
}
