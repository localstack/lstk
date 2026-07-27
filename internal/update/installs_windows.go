//go:build windows

package update

import (
	"os"
	"path/filepath"
	"strings"
)

// executableCandidates returns the lstk executables present in dir. On
// Windows a command resolves through PATHEXT (lstk.exe from a binary or
// scoop install, lstk.cmd from an npm shim), so each extension is probed.
// A bare extensionless "lstk" file is not executable by cmd.exe and is
// ignored, matching exec.LookPath. Lookups are case-insensitive on Windows
// filesystems, so probing the lowercased name finds any casing.
func executableCandidates(dir string, getenv func(string) string) []string {
	pathext := getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	var out []string
	for ext := range strings.SplitSeq(pathext, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		path := filepath.Join(dir, binaryName+strings.ToLower(ext))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, path)
	}
	return out
}
