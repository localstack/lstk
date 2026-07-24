//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
)

func writeFakeExecutable(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, binaryName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func pathGetenv(dirs ...string) func(string) string {
	return func(key string) string {
		if key == "PATH" {
			return strings.Join(dirs, string(os.PathListSeparator))
		}
		return ""
	}
}

func TestFindInstallsReportsDistinctInstallsInPathOrder(t *testing.T) {
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	exeA := writeFakeExecutable(t, dirA)
	exeB := writeFakeExecutable(t, dirB)

	installs := FindInstalls(pathGetenv(dirA, dirB))

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if installs[0].Path != exeA || installs[1].Path != exeB {
		t.Errorf("expected PATH order [%s %s], got [%s %s]", exeA, exeB, installs[0].Path, installs[1].Path)
	}
}

func TestFindInstallsDeduplicatesSymlinkAliases(t *testing.T) {
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	exeA := writeFakeExecutable(t, dirA)
	if err := os.Symlink(exeA, filepath.Join(dirB, binaryName)); err != nil {
		t.Fatal(err)
	}

	installs := FindInstalls(pathGetenv(dirA, dirB))

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
	if installs[0].Path != exeA {
		t.Errorf("expected first PATH hit %s, got %s", exeA, installs[0].Path)
	}
}

func TestFindInstallsDeduplicatesAsdfShimAlias(t *testing.T) {
	t.Parallel()
	asdfDataDir := t.TempDir()
	installBin := filepath.Join(asdfDataDir, "installs", "nodejs", "22.22.0", "bin")
	npmPackage := filepath.Join(asdfDataDir, "installs", "nodejs", "22.22.0", "lib", "node_modules", "@localstack", "lstk")
	shimsDir := filepath.Join(asdfDataDir, "shims")
	for _, dir := range []string{installBin, npmPackage, shimsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	launcher := writeFakeExecutable(t, npmPackage)
	installPath := filepath.Join(installBin, binaryName)
	if err := os.Symlink(launcher, installPath); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(shimsDir, binaryName)
	shim := "#!/usr/bin/env bash\n# asdf-plugin: nodejs 22.22.0\nexec asdf exec \"lstk\" \"$@\"\n"
	if err := os.WriteFile(shimPath, []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, dirs := range [][]string{{installBin, shimsDir}, {shimsDir, installBin}} {
		installs := FindInstalls(pathGetenv(dirs...))
		if len(installs) != 1 {
			t.Fatalf("expected asdf shim and npm launcher to be one install, got %d: %+v", len(installs), installs)
		}
		if installs[0].Path != filepath.Join(dirs[0], binaryName) {
			t.Errorf("expected first PATH hit to be reported, got %s", installs[0].Path)
		}
		if installs[0].Method != InstallNPM {
			t.Errorf("expected npm install, got %s", installs[0].Method)
		}
	}
}

func TestFindInstallsDeduplicatesRepeatedPathDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFakeExecutable(t, dir)

	installs := FindInstalls(pathGetenv(dir, dir))

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
}

func TestFindInstallsSkipsNonExecutableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, binaryName), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if installs := FindInstalls(pathGetenv(dir)); len(installs) != 0 {
		t.Fatalf("expected no installs, got %+v", installs)
	}
}

func TestFindInstallsSkipsDirectoriesNamedLikeBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, binaryName), 0o755); err != nil {
		t.Fatal(err)
	}

	if installs := FindInstalls(pathGetenv(dir)); len(installs) != 0 {
		t.Fatalf("expected no installs, got %+v", installs)
	}
}

func TestFindInstallsSkipsEmptyAndRelativePathEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFakeExecutable(t, dir)

	installs := FindInstalls(pathGetenv("", "relative/dir", dir))

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
}

func TestFindInstallsSkipsBrokenSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, binaryName)); err != nil {
		t.Fatal(err)
	}

	if installs := FindInstalls(pathGetenv(dir)); len(installs) != 0 {
		t.Fatalf("expected no installs, got %+v", installs)
	}
}

func TestFindInstallsClassifiesInstallMethod(t *testing.T) {
	t.Parallel()
	brewDir := filepath.Join(t.TempDir(), "Caskroom", "lstk", "1.0", "bin")
	npmDir := filepath.Join(t.TempDir(), "node_modules", ".bin")
	for _, d := range []string{brewDir, npmDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFakeExecutable(t, d)
	}

	installs := FindInstalls(pathGetenv(brewDir, npmDir))

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if installs[0].Method != InstallHomebrew {
		t.Errorf("expected homebrew, got %s", installs[0].Method)
	}
	if installs[1].Method != InstallNPM {
		t.Errorf("expected npm, got %s", installs[1].Method)
	}
}

func TestFindInstallsMarksRunningExecutable(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dirA, binaryName)); err != nil {
		t.Fatal(err)
	}
	writeFakeExecutable(t, dirB)

	installs := FindInstalls(pathGetenv(dirA, dirB))

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if !installs[0].Running {
		t.Error("expected symlink to the test binary to be marked running")
	}
	if installs[1].Running {
		t.Error("expected unrelated executable to not be marked running")
	}
}

func TestSameNodeModulesTree(t *testing.T) {
	t.Parallel()
	launcher := "/home/u/.nvm/versions/node/v22/lib/node_modules/@localstack/lstk/launcher.js"
	goBinary := "/home/u/.nvm/versions/node/v22/lib/node_modules/@localstack/lstk-linux-x64/bin/lstk"
	otherTree := "/opt/other/node_modules/@localstack/lstk/launcher.js"
	plain := "/usr/local/bin/lstk"

	if !sameNodeModulesTree(launcher, goBinary) {
		t.Error("expected launcher and platform binary in the same node_modules tree to match")
	}
	if sameNodeModulesTree(launcher, otherTree) {
		t.Error("expected different node_modules trees to not match")
	}
	if sameNodeModulesTree(launcher, plain) {
		t.Error("expected non-npm path to not match")
	}
}

func TestWarnMultipleInstalls(t *testing.T) {
	t.Parallel()
	dirA, dirB := t.TempDir(), t.TempDir()
	exeA := writeFakeExecutable(t, dirA)
	writeFakeExecutable(t, dirB)

	var events []output.Event
	sink := output.SinkFunc(func(e output.Event) { events = append(events, e) })

	WarnMultipleInstalls(sink, pathGetenv(dirA))
	if len(events) != 0 {
		t.Fatalf("expected no warning for a single install, got %+v", events)
	}

	WarnMultipleInstalls(sink, pathGetenv(dirA, dirB))
	if len(events) != 1 {
		t.Fatalf("expected exactly one warning event, got %d", len(events))
	}
	ev, ok := events[0].(output.MultipleInstallsEvent)
	if !ok {
		t.Fatalf("expected MultipleInstallsEvent, got %T", events[0])
	}
	if len(ev.Installs) != 2 {
		t.Fatalf("expected 2 install locations, got %+v", ev.Installs)
	}
	if ev.Installs[0].Path != exeA {
		t.Errorf("expected first location %s, got %s", exeA, ev.Installs[0].Path)
	}
	if ev.Installs[0].Method != "binary" {
		t.Errorf("expected method binary, got %s", ev.Installs[0].Method)
	}
}
