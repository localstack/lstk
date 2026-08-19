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
	InstallExternal                      // owned by a third-party package or version manager
)

func (m InstallMethod) String() string {
	switch m {
	case InstallHomebrew:
		return "homebrew"
	case InstallNPM:
		return "npm"
	case InstallExternal:
		return "external"
	default:
		return "binary"
	}
}

// ExternalManager identifies the package manager that owns the lstk binary,
// empty unless Method is InstallExternal.
//
// lstk must not update these itself: replacing the binary would leave the
// manager's registry pointing at a version it no longer installed, or fail
// outright against Nix's read-only store. Homebrew and npm are absent because
// lstk drives those through `brew upgrade` / `npm install -g`.
type ExternalManager string

const (
	ManagerNix        ExternalManager = "nix"
	ManagerMise       ExternalManager = "mise"
	ManagerASDF       ExternalManager = "asdf"
	ManagerScoop      ExternalManager = "scoop"
	ManagerChocolatey ExternalManager = "chocolatey"
)

// externalManagers holds the user-facing facts per manager, one row each.
//
// upgradeCommand is empty for Nix and asdf on purpose: a Nix install may be a
// profile, a nixos-rebuild generation or home-manager, and asdf has no `upgrade`
// verb. Printing a command that fails is worse than naming the manager.
var externalManagers = map[ExternalManager]struct {
	displayName    string
	upgradeCommand string
}{
	ManagerNix:        {"Nix", ""},
	ManagerMise:       {"mise", "mise upgrade lstk"},
	ManagerASDF:       {"asdf", ""},
	ManagerScoop:      {"Scoop", "scoop update lstk"},
	ManagerChocolatey: {"Chocolatey", "choco upgrade lstk"},
}

// DisplayName is the manager's name as its own project capitalizes it.
func (m ExternalManager) DisplayName() string {
	if entry, ok := externalManagers[m]; ok {
		return entry.displayName
	}
	return string(m)
}

func (m ExternalManager) UpgradeCommand() string {
	return externalManagers[m].upgradeCommand
}

// UpgradeAdvice is a clause for use inside a sentence: "run mise upgrade lstk",
// or "update it with Nix" when no single command applies.
func (m ExternalManager) UpgradeAdvice() string {
	if cmd := m.UpgradeCommand(); cmd != "" {
		return "run " + cmd
	}
	return "update it with " + m.DisplayName()
}

// InstallInfo holds the detected install method and the resolved binary path.
type InstallInfo struct {
	Method       InstallMethod
	Manager      ExternalManager // empty unless Method is InstallExternal
	ResolvedPath string
}

// ExternallyManaged means lstk must never replace this binary.
func (i InstallInfo) ExternallyManaged() bool {
	return i.Method == InstallExternal
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
	return classifyPath(resolved)
}

type externalManagerMarker struct {
	manager ExternalManager
	// followedBy are the segments that may come next. Empty means the segment
	// name alone is conclusive.
	followedBy []string
}

// externalManagerBySegment maps a lowercased path segment to its manager.
//
// Most entries require a following segment: a bare "mise" or "scoop" directory
// is more likely a checkout of that tool than an lstk it installed, and a false
// positive means `lstk update` refuses and advises a command the user cannot
// run. The dot-prefixed names are specific enough alone.
var externalManagerBySegment = map[string]externalManagerMarker{
	"mise":         {ManagerMise, []string{"installs", "shims", "tools"}},
	"asdf":         {ManagerASDF, []string{"installs", "shims"}},
	"scoop":        {ManagerScoop, []string{"apps", "shims"}},
	"chocolatey":   {ManagerChocolatey, []string{"lib", "bin"}},
	"nix":          {ManagerNix, []string{"store"}},
	".asdf":        {ManagerASDF, nil},
	".nix-profile": {ManagerNix, nil},
}

// classifyPath derives the install method from the resolved executable path.
//
// The loop order is the contract: lstk's own install methods match before any
// manager segment, because an npm-installed lstk under a mise-managed Node.js is
// still an npm install and must keep updating through npm.
//
// Detection is path-only by design. A write-permission probe cannot tell a
// root-owned /usr/local (self-managed, needs sudo) from a manager-owned
// directory, and the two need different advice. Nix profiles resolve through
// EvalSymlinks into /nix/store, so matching the store covers them.
func classifyPath(resolved string) InstallInfo {
	// Every marker is lowercase; ResolvedPath keeps the original casing.
	segments := splitPathSegments(strings.ToLower(resolved))

	for _, seg := range segments {
		switch seg {
		case "caskroom":
			return InstallInfo{Method: InstallHomebrew, ResolvedPath: resolved}
		case "node_modules":
			return InstallInfo{Method: InstallNPM, ResolvedPath: resolved}
		}
	}

	for i, seg := range segments {
		marker, ok := externalManagerBySegment[seg]
		if !ok {
			continue
		}
		if len(marker.followedBy) > 0 && (i+1 >= len(segments) || !slices.Contains(marker.followedBy, segments[i+1])) {
			continue
		}
		return InstallInfo{Method: InstallExternal, Manager: marker.manager, ResolvedPath: resolved}
	}

	return InstallInfo{Method: InstallBinary, ResolvedPath: resolved}
}

// splitPathSegments splits on both separators regardless of host OS, so a
// Windows path classifies (and tests) on Linux and vice versa. A Unix filename
// containing a backslash splits too; harmless, since no marker can result.
func splitPathSegments(path string) []string {
	return strings.FieldsFunc(filepath.Clean(path), func(r rune) bool {
		return r == '/' || r == '\\'
	})
}
