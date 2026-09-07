package update

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type InstallMethod int

const (
	InstallBinary   InstallMethod = iota // standalone binary download
	InstallHomebrew                      // installed via Homebrew cask
	InstallNPM                           // installed via npm
	InstallExternal                      // managed by an external tool (nix, mise, ...)
)

// InstallInfo holds the detected install method and the resolved binary path.
type InstallInfo struct {
	Method       InstallMethod
	Manager      string
	ResolvedPath string
}

// DetectInstallMethod determines how lstk was installed by inspecting the
// resolved path of the running binary.
func DetectInstallMethod() InstallInfo {
	exe, err := os.Executable()
	if err != nil {
		return InstallInfo{Method: InstallBinary}
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	method, manager := classifyPath(resolved)
	return InstallInfo{
		Method:       method,
		Manager:      manager,
		ResolvedPath: resolved,
	}
}

// externalMarker identifies an externally-managed install by an adjacent pair
// of path segments: `first` immediately followed by any of `second`. Requiring
// two adjacent segments rather than one keeps an unrelated directory that
// happens to be called "mise" or "scoop" from being read as an install root.
type externalMarker struct {
	first   string
	second  []string
	manager string
}

// externalMarkers covers tool managers whose whole purpose is to own the
// version of the binary they installed, and immutable stores lstk cannot write
// to at all. `rtx` is mise's former directory name and reports as mise, since
// that is the tool the user would run.
// Each manager lists both its install root and the launcher directory that is
// actually on PATH — `shims`/`bin` entries are not symlinks into the install
// root on every platform (scoop's shims are launcher executables, asdf's are
// shell scripts), so EvalSymlinks does not rewrite them and the install-root
// marker alone would miss the common case.
var externalMarkers = []externalMarker{
	{first: "nix", second: []string{"store"}, manager: "nix"},
	{first: "gnu", second: []string{"store"}, manager: "guix"},
	{first: "mise", second: []string{"installs", "shims"}, manager: "mise"},
	{first: "rtx", second: []string{"installs", "shims"}, manager: "mise"},
	// Both layouts: ~/.asdf and the ASDF_DATA_DIR convention
	// (~/.local/share/asdf) that Homebrew's asdf formula documents.
	{first: ".asdf", second: []string{"installs", "shims"}, manager: "asdf"},
	{first: "asdf", second: []string{"installs", "shims"}, manager: "asdf"},
	{first: "scoop", second: []string{"apps", "shims"}, manager: "scoop"},
	{first: "chocolatey", second: []string{"lib", "bin"}, manager: "chocolatey"},
}

// classifyPath determines the install method from a resolved executable path,
// returning the recognized external manager's name for InstallExternal and an
// empty string for every other method.
//
// The npm and Homebrew markers are checked across the whole path *before* any
// external marker, and that order is load-bearing: an npm- or Homebrew-managed
// lstk may sit under a tool-manager-provisioned interpreter or prefix (e.g.
// .../mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_.../lstk),
// where the tool manager owns node but `npm install -g` still updates lstk
// correctly. A single in-order segment walk would see "mise" first and refuse
// to update a perfectly updatable install.
func classifyPath(resolved string) (InstallMethod, string) {
	cleaned := filepath.Clean(resolved)
	segments := strings.Split(cleaned, string(os.PathSeparator))

	for _, seg := range segments {
		lower := strings.ToLower(seg)
		if lower == "caskroom" {
			return InstallHomebrew, ""
		}
		if lower == "node_modules" {
			return InstallNPM, ""
		}
	}

	for i, seg := range segments {
		if i+1 >= len(segments) {
			break
		}
		lower := strings.ToLower(seg)
		next := strings.ToLower(segments[i+1])
		for _, m := range externalMarkers {
			if lower != m.first {
				continue
			}
			if slices.Contains(m.second, next) {
				return InstallExternal, m.manager
			}
		}
	}

	return InstallBinary, ""
}
