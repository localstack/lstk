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
	cleaned := filepath.Clean(resolved)
	segments := strings.Split(cleaned, string(os.PathSeparator))

	for _, seg := range segments {
		lower := strings.ToLower(seg)
		if lower == "caskroom" {
			return InstallHomebrew
		}
		if lower == "node_modules" {
			return InstallNPM
		}
	}

	return InstallBinary
}
