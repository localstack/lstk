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

// ExternalManager identifies the third-party package or version manager that
// owns the lstk binary. It is empty for every method other than
// InstallExternal.
//
// These are managers lstk must not update on the user's behalf: replacing the
// binary in place would leave the manager's own registry pointing at a version
// it no longer installed (mise, asdf, Scoop, Chocolatey), or fail outright
// against a read-only store (Nix). Homebrew and npm are deliberately absent —
// lstk drives those through `brew upgrade` / `npm install -g`, so they stay
// self-updatable.
type ExternalManager string

const (
	ManagerNix        ExternalManager = "nix"
	ManagerMise       ExternalManager = "mise"
	ManagerASDF       ExternalManager = "asdf"
	ManagerScoop      ExternalManager = "scoop"
	ManagerChocolatey ExternalManager = "chocolatey"
)

// DisplayName is how the manager is named in user-facing output, using each
// project's own capitalization.
func (m ExternalManager) DisplayName() string {
	switch m {
	case ManagerNix:
		return "Nix"
	case ManagerMise:
		return "mise"
	case ManagerASDF:
		return "asdf"
	case ManagerScoop:
		return "Scoop"
	case ManagerChocolatey:
		return "Chocolatey"
	default:
		return string(m)
	}
}

// UpgradeCommand is the manager's own command for upgrading lstk, or empty when
// there is no single correct one.
//
// Nix and asdf deliberately return nothing: a Nix install may be a profile, a
// nixos-rebuild generation or home-manager, and asdf has no `upgrade` verb (nor
// an lstk plugin). Printing a command that fails is worse than naming the
// manager and stopping there — callers fall back to UpgradeAdvice.
func (m ExternalManager) UpgradeCommand() string {
	switch m {
	case ManagerMise:
		return "mise upgrade lstk"
	case ManagerScoop:
		return "scoop update lstk"
	case ManagerChocolatey:
		return "choco upgrade lstk"
	default:
		return ""
	}
}

// UpgradeAdvice is the imperative clause telling the user how to update through
// the manager that owns the binary, for use inside a sentence: "run mise upgrade
// lstk", or "update it with Nix" when no single command applies.
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

// ExternallyManaged reports whether another tool owns this binary, meaning lstk
// must never replace it in place.
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

// externalManagerMarker names a manager and, when its directory name is not
// distinctive enough on its own, the segments that may follow it.
type externalManagerMarker struct {
	manager ExternalManager
	// followedBy lists the segment names that must come next for the match to
	// count. Empty means the segment name alone is conclusive.
	followedBy []string
}

// externalManagerBySegment maps a lowercased path segment to the manager it
// identifies.
//
// Most entries require a following segment, because a bare "mise" or "scoop"
// directory is more likely a checkout of that tool than an install of lstk by
// it — and a false positive costs the user a refusal from `lstk update` advising
// a command that does not apply to them. The dot-prefixed names are specific
// enough to stand alone.
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
// The ordering is the contract: lstk's own install methods are matched before
// any manager segment, because an npm-installed lstk under a mise- or
// asdf-managed Node.js is still an npm install and must keep updating itself
// through npm. Reversing these two loops would break that case.
//
// Detection is deliberately path-only. A write-permission probe was considered
// and rejected: it cannot tell a root-owned /usr/local (self-managed, needs
// sudo) from a manager-owned directory, and the two need different advice.
//
// NixOS system profiles (/run/current-system/sw/bin/lstk) and per-user profiles
// resolve through the EvalSymlinks in DetectInstallMethod into /nix/store, so
// matching the store covers them without listing every profile path.
func classifyPath(resolved string) InstallInfo {
	segments := splitPathSegments(resolved)
	for i, seg := range segments {
		segments[i] = strings.ToLower(seg)
	}

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
		if len(marker.followedBy) > 0 {
			if i+1 >= len(segments) || !slices.Contains(marker.followedBy, segments[i+1]) {
				continue
			}
		}
		return InstallInfo{Method: InstallExternal, Manager: marker.manager, ResolvedPath: resolved}
	}

	return InstallInfo{Method: InstallBinary, ResolvedPath: resolved}
}

// splitPathSegments splits a path on both separators regardless of the host OS,
// so a Windows path can be classified (and tested) on Linux and vice versa. A
// Unix filename containing a literal backslash is split too; that false split
// is accepted, since no manager segment can result from it.
func splitPathSegments(path string) []string {
	return strings.FieldsFunc(filepath.Clean(path), func(r rune) bool {
		return r == '/' || r == '\\'
	})
}
