//go:build !windows

package update

import (
	"os"
	"path/filepath"
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
