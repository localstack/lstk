//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractorsPreserveArchiveModesRegardlessOfUmask pins that extraction
// restores the archive's file modes explicitly: the mode passed to OpenFile is
// masked by the process umask, and discoverMembers keys extension discovery
// off the extracted exec bits, so without the explicit Chmod a restrictive
// umask would silently shrink the installed set.
//
// Not parallel: umask is process-wide, and Go releases paused parallel tests
// only after the sequential pass finishes, so changing it here cannot race
// another test's file creation.
func TestExtractorsPreserveArchiveModesRegardlessOfUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	for _, format := range archiveFormats {
		t.Run(format, func(t *testing.T) {
			archive := buildArchive(t, format, []archiveEntry{
				{name: "lstk", body: "new lstk", mode: 0o755},
				{name: "lstk-alpha", body: "new alpha", mode: 0o755},
			})

			dest := t.TempDir()
			if format == "zip" {
				require.NoError(t, extractZip(archive, dest))
			} else {
				require.NoError(t, extractTarGz(archive, dest))
			}

			for _, name := range []string{"lstk", "lstk-alpha"} {
				info, err := os.Stat(filepath.Join(dest, name))
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o755), info.Mode().Perm(),
					"%s should keep its archive mode despite the umask", name)
			}
		})
	}
}
