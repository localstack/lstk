//go:build windows

package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func winGetenv(pathext string, dirs ...string) func(string) string {
	return func(key string) string {
		switch key {
		case "PATH":
			return strings.Join(dirs, string(os.PathListSeparator))
		case "PATHEXT":
			return pathext
		}
		return ""
	}
}

func TestFindInstallsProbesPathext(t *testing.T) {
	t.Parallel()
	dirExe, dirCmd := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dirExe, "lstk.exe"))
	writeFile(t, filepath.Join(dirCmd, "lstk.cmd"))
	// A bare extensionless file (npm's git-bash script) must not count.
	writeFile(t, filepath.Join(dirCmd, "lstk"))

	installs := FindInstalls(winGetenv(".COM;.EXE;.BAT;.CMD", dirExe, dirCmd))

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if !strings.HasSuffix(installs[0].Path, "lstk.exe") {
		t.Errorf("expected lstk.exe first, got %s", installs[0].Path)
	}
	if !strings.HasSuffix(installs[1].Path, "lstk.cmd") {
		t.Errorf("expected lstk.cmd second, got %s", installs[1].Path)
	}
}

func TestFindInstallsDefaultPathextWhenUnset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lstk.exe"))

	installs := FindInstalls(winGetenv("", dir))

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
}

func TestFindInstallsPathextOrderWithinDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lstk.exe"))
	writeFile(t, filepath.Join(dir, "lstk.cmd"))

	installs := FindInstalls(winGetenv(".COM;.EXE;.BAT;.CMD", dir))

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if !strings.HasSuffix(installs[0].Path, "lstk.exe") {
		t.Errorf("expected PATHEXT order to put lstk.exe first, got %s", installs[0].Path)
	}
}
