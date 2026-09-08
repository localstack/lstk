package update

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/localstack/lstk/internal/version"
)

// MissingBundle describes a binary install of a bundling release whose bundled
// extensions are not beside lstk: what the pre-bundling updater leaves behind
// when it installs a bundling release. Homebrew and npm replace the whole
// package, so they never end up here.
type MissingBundle struct {
	Dir       string // install directory that should hold the bundle
	Reinstall string // what restores the complete set
}

const reinstallInstruction = "download the latest release from https://github.com/localstack/lstk/releases/latest"

// Summary is the one-sentence explanation shown wherever the state is reported.
func (m MissingBundle) Summary() string {
	return fmt.Sprintf("This lstk release ships bundled extensions, but none are installed in %s.", m.Dir)
}

// DetectMissingBundle reports whether the running lstk is a release build
// (version.BundlesExtensions), installed as a plain binary, with no bundle
// beside it. Dev builds never report one.
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

// DetectMissingBundleFor is DetectMissingBundle limited to the commands the
// bundle provided at the time of the transition, so a typo never earns a
// reinstall hint. Retire it with the hint once every supported release ships
// the set-wise updater.
func DetectMissingBundleFor(command string) (MissingBundle, bool) {
	switch command {
	case "deploy", "doctor":
		return DetectMissingBundle()
	}
	return MissingBundle{}, false
}

func detectMissingBundle(info InstallInfo, goos string) (MissingBundle, bool) {
	if info.Method != InstallBinary {
		return MissingBundle{}, false
	}
	dir := filepath.Dir(info.ResolvedPath)
	if !bundleMissing(dir, goos) {
		return MissingBundle{}, false
	}
	return MissingBundle{Dir: dir, Reinstall: reinstallInstruction}, true
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
