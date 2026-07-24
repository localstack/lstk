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

// executableAlias resolves an asdf shim to the matching installed executable
// when both appear on PATH. asdf shims are dispatcher scripts, not separate
// installations, but os.SameFile cannot identify that relationship.
func executableAlias(candidate string, candidates []string) string {
	for _, target := range asdfShimTargets(candidate) {
		for _, other := range candidates {
			if filepath.Clean(target) == filepath.Clean(other) {
				return other
			}
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
