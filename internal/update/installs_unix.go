//go:build !windows

package update

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// executableCandidates returns the lstk executables present in dir. On Unix
// that is the single file named lstk, when it is a regular file with an
// execute bit set — the same test exec.LookPath applies. os.Stat follows
// symlinks, so a symlink to an executable qualifies and a broken one is
// skipped.
func executableCandidates(dir string, _ func(string) string) []string {
	path := filepath.Join(dir, binaryName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return nil
	}
	return []string{path}
}

// executableAlias resolves a version-manager shim to the matching installed
// executable when both appear on PATH. Shims are dispatchers, not separate
// installations, but os.SameFile cannot identify that relationship.
func executableAlias(candidate string, candidates []string) string {
	if filepath.Base(filepath.Dir(candidate)) != "shims" {
		return candidate
	}
	// asdf shims are scripts naming their backing plugin/version in a
	// comment; prefer that exact mapping.
	for _, target := range asdfShimTargets(candidate) {
		for _, other := range candidates {
			if filepath.Clean(target) == filepath.Clean(other) {
				return other
			}
		}
	}
	return dispatcherShimAlias(candidate, candidates)
}

// dispatcherShimAlias handles argv[0]-dispatch shims: mise shims are symlinks
// to the mise binary itself, so neither file identity nor shim content can
// reveal the backing install. A shim symlink-resolving to a foreign-named
// executable is such a dispatcher; it aliases to the first PATH candidate
// inside the sibling installs/ tree (shims/ and installs/ always share the
// mise data dir — there is no override that separates them). The true
// dispatch target is resolved by mise per-invocation and cannot be known
// here, so a candidate from the same installs/ tree is trusted to be it;
// anything under that tree is managed by the same version manager, not a
// competing installation this warning is meant to catch.
func dispatcherShimAlias(candidate string, candidates []string) string {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || filepath.Base(resolved) == binaryName {
		return candidate
	}
	installsPrefix := filepath.Join(filepath.Dir(filepath.Dir(candidate)), "installs") + string(filepath.Separator)
	for _, other := range candidates {
		if other != candidate && strings.HasPrefix(filepath.Clean(other), installsPrefix) {
			return other
		}
	}
	return candidate
}

func asdfShimTargets(candidate string) []string {
	shimsDir := filepath.Dir(candidate)
	if filepath.Base(shimsDir) != "shims" {
		return nil
	}

	f, err := os.Open(candidate)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	content, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return nil
	}

	asdfDataDir := filepath.Dir(shimsDir)
	var targets []string
	for line := range strings.SplitSeq(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != "#" || fields[1] != "asdf-plugin:" {
			continue
		}
		plugin, version := fields[2], fields[3]
		if !isPathSegment(plugin) || !isPathSegment(version) {
			continue
		}
		targets = append(targets, filepath.Join(asdfDataDir, "installs", plugin, version, "bin", binaryName))
	}
	return targets
}

func isPathSegment(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value
}
