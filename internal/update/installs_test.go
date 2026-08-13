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

func valuesGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func fakeASDFInstall(t *testing.T) (string, string, string) {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), ".asdf")
	installPath := filepath.Join(dataDir, "installs", "nodejs", "22.22.0")
	binDir := filepath.Join(installPath, "bin")
	shimDir := filepath.Join(dataDir, "shims")
	packageDir := filepath.Join(installPath, "lib", "node_modules", "@localstack", "lstk")
	for _, dir := range []string{binDir, shimDir, packageDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(packageDir, "index.js")
	if err := os.WriteFile(launcher, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(launcher, filepath.Join(binDir, binaryName)); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimDir, binaryName)
	if err := os.WriteFile(shim, []byte("#!/usr/bin/env bash\n# asdf-plugin: nodejs 22.22.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedLauncher, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		t.Fatal(err)
	}
	return dataDir, installPath, resolvedLauncher
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

func TestFindInstallsDeduplicatesRepeatedPathDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFakeExecutable(t, dir)

	installs := FindInstalls(pathGetenv(dir, dir))

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
}

func TestFindInstallsDeduplicatesASDFShimAfterInstall(t *testing.T) {
	t.Parallel()
	dataDir, installPath, launcher := fakeASDFInstall(t)
	binDir := filepath.Join(installPath, "bin")
	shimDir := filepath.Join(dataDir, "shims")
	getenv := valuesGetenv(map[string]string{
		"PATH":              strings.Join([]string{binDir, shimDir}, string(os.PathListSeparator)),
		"ASDF_DATA_DIR":     dataDir,
		"ASDF_INSTALL_PATH": installPath,
	})

	installs := FindInstalls(getenv)

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
	if installs[0].Path != filepath.Join(binDir, binaryName) {
		t.Errorf("expected install path to be preserved, got %s", installs[0].Path)
	}
	if installs[0].ResolvedPath != launcher {
		t.Errorf("expected resolved path %s, got %s", launcher, installs[0].ResolvedPath)
	}
	if installs[0].Method != InstallNPM {
		t.Errorf("expected npm install, got %s", installs[0].Method)
	}
}

func TestFindInstallsDeduplicatesASDFShimBeforeInstall(t *testing.T) {
	t.Parallel()
	dataDir, installPath, launcher := fakeASDFInstall(t)
	binDir := filepath.Join(installPath, "bin")
	shimDir := filepath.Join(dataDir, "shims")
	getenv := valuesGetenv(map[string]string{
		"PATH":              strings.Join([]string{shimDir, binDir}, string(os.PathListSeparator)),
		"ASDF_DATA_DIR":     dataDir,
		"ASDF_INSTALL_PATH": installPath,
	})

	installs := FindInstalls(getenv)

	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d: %+v", len(installs), installs)
	}
	if installs[0].Path != filepath.Join(shimDir, binaryName) {
		t.Errorf("expected shim path to be preserved, got %s", installs[0].Path)
	}
	if installs[0].ResolvedPath != launcher {
		t.Errorf("expected resolved path %s, got %s", launcher, installs[0].ResolvedPath)
	}
	if installs[0].Method != InstallNPM {
		t.Errorf("expected npm install, got %s", installs[0].Method)
	}
}

func TestFindInstallsKeepsUnresolvedASDFShim(t *testing.T) {
	t.Parallel()
	dataDir, installPath, _ := fakeASDFInstall(t)
	binDir := filepath.Join(installPath, "bin")
	shimDir := filepath.Join(dataDir, "shims")

	for _, test := range []struct {
		name        string
		installPath string
	}{
		{name: "unset install path"},
		{name: "missing install path", installPath: filepath.Join(dataDir, "missing")},
	} {
		t.Run(test.name, func(t *testing.T) {
			getenv := valuesGetenv(map[string]string{
				"PATH":              strings.Join([]string{binDir, shimDir}, string(os.PathListSeparator)),
				"ASDF_DATA_DIR":     dataDir,
				"ASDF_INSTALL_PATH": test.installPath,
			})

			installs := FindInstalls(getenv)

			if len(installs) != 2 {
				t.Fatalf("expected 2 distinct entries, got %d: %+v", len(installs), installs)
			}
		})
	}
}

func TestFindInstallsReportsASDFShimAndDistinctInstall(t *testing.T) {
	t.Parallel()
	dataDir, installPath, _ := fakeASDFInstall(t)
	shimDir := filepath.Join(dataDir, "shims")
	standaloneDir := t.TempDir()
	standalone := writeFakeExecutable(t, standaloneDir)
	getenv := valuesGetenv(map[string]string{
		"PATH":              strings.Join([]string{shimDir, standaloneDir}, string(os.PathListSeparator)),
		"ASDF_DATA_DIR":     dataDir,
		"ASDF_INSTALL_PATH": installPath,
	})

	installs := FindInstalls(getenv)

	if len(installs) != 2 {
		t.Fatalf("expected 2 installs, got %d: %+v", len(installs), installs)
	}
	if installs[0].Path != filepath.Join(shimDir, binaryName) || installs[0].Method != InstallNPM {
		t.Errorf("expected asdf npm install first, got %+v", installs[0])
	}
	if installs[1].Path != standalone || installs[1].Method != InstallBinary {
		t.Errorf("expected standalone binary second, got %+v", installs[1])
	}
}

func TestFindInstallsDoesNotResolveASDFShimForDifferentInstall(t *testing.T) {
	t.Parallel()
	dataDir, installPath, _ := fakeASDFInstall(t)
	binDir := filepath.Join(installPath, "bin")
	shimDir := filepath.Join(dataDir, "shims")
	shim := filepath.Join(shimDir, binaryName)
	if err := os.WriteFile(shim, []byte("#!/usr/bin/env bash\n# asdf-plugin: golang 1.26.5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	getenv := valuesGetenv(map[string]string{
		"PATH":              strings.Join([]string{binDir, shimDir}, string(os.PathListSeparator)),
		"ASDF_DATA_DIR":     dataDir,
		"ASDF_INSTALL_PATH": installPath,
	})

	installs := FindInstalls(getenv)

	if len(installs) != 2 {
		t.Fatalf("expected 2 distinct entries, got %d: %+v", len(installs), installs)
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
