package update

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// binaryName is the executable name scanned for on PATH.
const binaryName = "lstk"

// Install describes one distinct lstk executable found on PATH.
type Install struct {
	Path         string // location as found on PATH (what a shell would execute)
	ResolvedPath string // after symlink resolution
	Method       InstallMethod
	Running      bool // whether this entry is the currently running executable
}

// FindInstalls scans the directories in the PATH environment variable for
// lstk executables. Entries that resolve to the same file (symlinks,
// hardlinks, the same directory listed twice) are reported once. Results
// follow PATH order, so the first entry is the one a shell would execute.
func FindInstalls(getenv func(string) string) []Install {
	runningInfo, runningResolved := runningExecutable()

	var candidates []string
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		// Relative and empty entries (cwd-dependent lookup) are skipped: they
		// resolve differently per invocation and are not real install locations.
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidates = append(candidates, executableCandidates(dir, getenv)...)
	}

	var installs []Install
	var seen []os.FileInfo
	for _, candidate := range candidates {
		alias := executableAlias(candidate, candidates)
		resolved, err := filepath.EvalSymlinks(alias)
		if err != nil {
			resolved = alias
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
	return installs
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
