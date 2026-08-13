package update

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// binaryName is the executable name scanned for on PATH.
const binaryName = "lstk"

const maxASDFShimSize = 64 * 1024

// Install describes one distinct lstk executable found on PATH.
type Install struct {
	Path         string // location as found on PATH (what a shell would execute)
	ResolvedPath string // canonical executable target after shim and symlink resolution
	Method       InstallMethod
	Running      bool // whether this entry is the currently running executable
}

// FindInstalls scans the directories in the PATH environment variable for
// lstk executables. Entries that resolve to the same install (symlinks,
// hardlinks, active asdf shims, the same directory listed twice) are reported
// once. Results follow PATH order, so the first entry is what a shell executes.
func FindInstalls(getenv func(string) string) []Install {
	runningInfo, runningResolved := runningExecutable()

	var installs []Install
	var seen []os.FileInfo
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		// Relative and empty entries (cwd-dependent lookup) are skipped: they
		// resolve differently per invocation and are not real install locations.
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, candidate := range executableCandidates(dir, getenv) {
			installPath := resolveASDFShim(candidate, getenv)
			resolved, err := filepath.EvalSymlinks(installPath)
			if err != nil {
				resolved = installPath
			}
			info, err := os.Stat(resolved)
			if err != nil {
				continue
			}
			if isDuplicate(seen, info) {
				continue
			}
			seen = append(seen, info)
			installs = append(installs, Install{
				Path:         candidate,
				ResolvedPath: resolved,
				Method:       classifyPath(resolved),
				Running:      isRunning(info, resolved, runningInfo, runningResolved),
			})
		}
	}
	return installs
}

func resolveASDFShim(candidate string, getenv func(string) string) string {
	// asdf shims are wrapper scripts rather than symlinks, so resolve the
	// active shim explicitly before comparing file identities.
	dataDir := getenv("ASDF_DATA_DIR")
	if dataDir == "" {
		home := getenv("HOME")
		if home == "" {
			return candidate
		}
		dataDir = filepath.Join(home, ".asdf")
	}
	if filepath.Clean(filepath.Dir(candidate)) != filepath.Clean(filepath.Join(dataDir, "shims")) {
		return candidate
	}

	installPath := getenv("ASDF_INSTALL_PATH")
	if installPath == "" || !asdfShimSelectsInstall(candidate, dataDir, installPath) {
		return candidate
	}
	targets := executableCandidates(filepath.Join(installPath, "bin"), getenv)
	if len(targets) == 0 {
		return candidate
	}
	return targets[0]
}

func asdfShimSelectsInstall(candidate, dataDir, installPath string) bool {
	// ASDF_INSTALL_PATH can be inherited from an unrelated asdf command, so
	// confirm that this generated shim lists the same plugin and version.
	rel, err := filepath.Rel(filepath.Join(dataDir, "installs"), installPath)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.Clean(rel), string(os.PathSeparator))
	if len(parts) != 2 || parts[0] == ".." || parts[0] == "." || parts[1] == "." {
		return false
	}

	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxASDFShimSize {
		return false
	}
	contents, err := os.ReadFile(candidate)
	if err != nil {
		return false
	}
	want := "# asdf-plugin: " + parts[0] + " " + parts[1]
	for line := range strings.SplitSeq(string(contents), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// WarnMultipleInstalls emits a warning when more than one distinct lstk
// install is present on PATH (e.g. an old Homebrew install shadowing a fresh
// npm one, so "lstk" keeps resolving to the stale binary).
func WarnMultipleInstalls(sink output.Sink, getenv func(string) string) {
	installs := FindInstalls(getenv)
	if len(installs) < 2 {
		return
	}
	locations := make([]output.InstallLocation, len(installs))
	for i, in := range installs {
		locations[i] = output.InstallLocation{
			Path:    in.Path,
			Method:  in.Method.String(),
			Running: in.Running,
		}
	}
	sink.Emit(output.MultipleInstallsEvent{Installs: locations})
}

func runningExecutable() (os.FileInfo, string) {
	exe, err := os.Executable()
	if err != nil {
		return nil, ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, resolved
	}
	return info, resolved
}

func isDuplicate(seen []os.FileInfo, info os.FileInfo) bool {
	for _, s := range seen {
		if os.SameFile(s, info) {
			return true
		}
	}
	return false
}

func isRunning(info os.FileInfo, resolved string, runningInfo os.FileInfo, runningResolved string) bool {
	if runningInfo != nil && os.SameFile(runningInfo, info) {
		return true
	}
	// An npm install's PATH entry resolves to the Node launcher script while
	// the running process is the Go binary from the platform package —
	// different files inside the same node_modules tree.
	return sameNodeModulesTree(resolved, runningResolved)
}

func sameNodeModulesTree(a, b string) bool {
	rootA, okA := nodeModulesRoot(a)
	rootB, okB := nodeModulesRoot(b)
	return okA && okB && rootA == rootB
}

// nodeModulesRoot returns the path prefix up to and including the first
// node_modules segment, or ok=false when the path has none.
func nodeModulesRoot(p string) (string, bool) {
	segments := strings.Split(filepath.Clean(p), string(os.PathSeparator))
	for i, seg := range segments {
		if strings.EqualFold(seg, "node_modules") {
			return strings.Join(segments[:i+1], string(os.PathSeparator)), true
		}
	}
	return "", false
}
