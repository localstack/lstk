package update

import (
	"os"
	"path/filepath"
	"strings"
)

type InstallMethod int

const (
	InstallBinary   InstallMethod = iota // standalone binary download
	InstallHomebrew                      // installed via Homebrew cask
	InstallNPM                           // installed via npm
)

func (m InstallMethod) String() string {
	switch m {
	case InstallHomebrew:
		return "homebrew"
	case InstallNPM:
		return "npm"
	default:
		return "binary"
	}
}

// InstallInfo holds the detected install method and the resolved binary path.
type InstallInfo struct {
	Method       InstallMethod
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
	return InstallInfo{
		Method:       classifyPath(resolved),
		ResolvedPath: resolved,
	}
}

func classifyPath(resolved string) InstallMethod {
	lower := strings.ToLower(resolved)

	// Homebrew cask: symlink resolves into /Caskroom/
	if strings.Contains(lower, "/caskroom/") {
		return InstallHomebrew
	}

	// npm install: the Go binary lives inside a node_modules directory
	// e.g. <prefix>/lib/node_modules/@localstack/lstk_darwin_arm64/lstk
	if strings.Contains(lower, "node_modules") {
		return InstallNPM
	}

	return InstallBinary
}

// npmProjectDir returns the project directory for a local npm install,
// or empty string for a global install. A local install has a package.json
// in the parent of the node_modules directory.
func npmProjectDir(resolvedPath string) string {
	// Walk up to find node_modules, then check for package.json one level above
	dir := resolvedPath
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if filepath.Base(dir) == "node_modules" {
			pkgJSON := filepath.Join(parent, "package.json")
			if _, err := os.Stat(pkgJSON); err == nil {
				return parent
			}
			return ""
		}
		dir = parent
	}
	return ""
}
