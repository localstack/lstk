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
// two keeps an unrelated directory named "mise" or "scoop" from matching.
type externalMarker struct {
	first   string
	second  []string
	manager string
}

// externalMarkers covers tool managers that own the version of the binary they
// installed, plus immutable stores lstk cannot write to. `rtx` is mise's former
// directory name and reports as mise, the tool the user would run.
//
// Tool-manager entries list the install root *and* the launcher directory on
// PATH: `shims`/`bin` entries are not symlinks into the install root everywhere
// (scoop's are launcher exes, asdf's are shell scripts), so EvalSymlinks leaves
// them alone and the install-root marker would miss the common case. The store
// entries need only one segment pair — nothing is installed outside the store.
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
// naming the recognized external manager for InstallExternal and "" otherwise.
//
// npm and Homebrew markers are checked across the whole path before any
// external marker, and that order is load-bearing: an npm-installed lstk can
// sit under a tool-manager-provisioned interpreter
// (.../mise/installs/node/24.8.0/lib/node_modules/@localstack/lstk_.../lstk),
// where `npm install -g` still updates it correctly. A single in-order walk
// would see "mise" first and refuse an update that would have worked.
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
