package extension

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/log"
)

// writeBundle installs the multi-call bundled binary in dir plus a descriptions
// file naming the given commands, which is the on-disk shape a release ships.
func writeBundle(t *testing.T, dir string, names ...string) string {
	t.Helper()
	path := writeExe(t, dir, BundledBinaryName)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name + " = \"Description of " + name + "\"\n")
	}
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadBundleReadsCommandsFromDescriptions(t *testing.T) {
	dir := t.TempDir()
	path := writeBundle(t, dir, "doctor", "deploy")

	bundle, err := LoadBundle(dir, log.Nop())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle == nil {
		t.Fatal("expected a bundle")
	}
	if bundle.Path != path {
		t.Fatalf("path = %q, want %q", bundle.Path, path)
	}
	if !bundle.Provides("doctor") || !bundle.Provides("deploy") {
		t.Fatalf("expected doctor and deploy, got %v", bundle.Names())
	}
	if bundle.Provides("nope") {
		t.Fatal("expected an undescribed name not to be provided")
	}
}

func TestLoadBundleAbsentWhenNoBinary(t *testing.T) {
	dir := t.TempDir()
	// A descriptions file with no bundled binary is the pre-bundling shape.
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte("doctor = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := LoadBundle(dir, log.Nop())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle != nil {
		t.Fatalf("expected no bundle, got %+v", bundle)
	}
}

func TestLoadBundleEmptyDir(t *testing.T) {
	bundle, err := LoadBundle("", log.Nop())
	if err != nil || bundle != nil {
		t.Fatalf("expected no bundle and no error, got %+v / %v", bundle, err)
	}
}

// The descriptions file is the only record of which commands the bundled binary
// provides, so unlike the lenient help path it must not degrade to "no
// extensions" when the binary is present.
func TestLoadBundleMissingDescriptionsIsHardError(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, BundledBinaryName)

	if _, err := LoadBundle(dir, log.Nop()); err == nil {
		t.Fatal("expected an error when the descriptions file is missing")
	}
}

func TestLoadBundleMalformedDescriptionsIsHardError(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, BundledBinaryName)
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte("not = valid = toml ="), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBundle(dir, log.Nop()); err == nil {
		t.Fatal("expected an error for a malformed descriptions file")
	}
}

func TestLoadBundleEmptyDescriptionsIsHardError(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, BundledBinaryName)
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBundle(dir, log.Nop()); err == nil {
		t.Fatal("expected an error when the bundle describes no commands")
	}
}

func TestResolveBundledMultiCallSetsArgv0(t *testing.T) {
	dir := t.TempDir()
	path := writeBundle(t, dir, "doctor")
	t.Setenv("PATH", t.TempDir())

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	ext, err := r.Resolve("doctor")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ext.Path != path {
		t.Fatalf("path = %q, want the bundled binary %q", ext.Path, path)
	}
	if !ext.Bundled {
		t.Fatal("expected the extension to be marked bundled")
	}
	// argv[0] is the whole mechanism: it is how the one binary knows which
	// extension it is being asked to be.
	if want := NamePrefix + "doctor"; ext.Argv0 != want {
		t.Fatalf("argv0 = %q, want %q", ext.Argv0, want)
	}
}

func TestResolveBundledMultiCallWinsOverPath(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	bundlePath := writeBundle(t, dir, "doctor")
	writeExe(t, pathDir, "lstk-doctor")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	ext, err := r.Resolve("doctor")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ext.Path != bundlePath {
		t.Fatalf("expected the bundle to win, got %q", ext.Path)
	}
}

// An undescribed name is not part of the bundled set, so resolution must fall
// through to PATH rather than handing an unknown command to the bundle.
func TestResolveUndescribedNameFallsThroughToPath(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	writeBundle(t, dir, "doctor")
	writeExe(t, pathDir, "lstk-other")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	ext, err := r.Resolve("other")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ext.Bundled {
		t.Fatalf("expected the PATH extension, got bundled %+v", ext)
	}
}

func TestResolveUndescribedNameNotFound(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "doctor")
	t.Setenv("PATH", t.TempDir())

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	if _, err := r.Resolve("other"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// A shipped bundle whose descriptions file cannot be read is a broken install,
// not an absent extension: reporting "unknown command" would hide it.
func TestResolveBrokenBundleReturnsError(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, BundledBinaryName)
	t.Setenv("PATH", t.TempDir())

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	_, err := r.Resolve("doctor")
	if err == nil || err == ErrNotFound {
		t.Fatalf("expected a descriptive error, got %v", err)
	}
}

func TestListIncludesBundledMultiCallCommands(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	writeBundle(t, dir, "doctor", "deploy")
	writeExe(t, pathDir, "lstk-hello")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	list := r.List()

	if len(list) != 3 {
		t.Fatalf("expected 3 extensions, got %d: %+v", len(list), list)
	}
	// Sorted by name: deploy, doctor, hello.
	for i, want := range []string{"deploy", "doctor", "hello"} {
		if list[i].Name != want {
			t.Fatalf("list[%d].Name = %q, want %q", i, list[i].Name, want)
		}
	}
	if !list[0].Bundled || !list[1].Bundled || list[2].Bundled {
		t.Fatalf("unexpected bundled flags: %+v", list)
	}
	if list[0].Path != list[1].Path {
		t.Fatal("expected both bundled commands to point at the one binary")
	}
}

// The bundled binary itself is not an extension named "bundled-extensions", and
// the descriptions file is not an extension named "extensions".
func TestListDoesNotListBundleArtifactsAsExtensions(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "doctor")
	t.Setenv("PATH", t.TempDir())

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	for _, ext := range r.List() {
		if ext.Name != "doctor" {
			t.Fatalf("unexpected extension listed: %+v", ext)
		}
	}
}

// Help rendering must never fail on account of a broken bundle, so List
// degrades where Resolve reports.
func TestListDegradesOnBrokenBundle(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	writeExe(t, dir, BundledBinaryName)
	writeExe(t, pathDir, "lstk-hello")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	list := r.List()
	if len(list) != 1 || list[0].Name != "hello" {
		t.Fatalf("expected only the PATH extension, got %+v", list)
	}
}

// Manually placed lstk-<name> files in the install directory keep working
// alongside the bundle, which is how the mechanism shipped before bundling.
func TestResolveManuallyPlacedBundledFileStillWorks(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "doctor")
	manual := writeExe(t, dir, "lstk-manual")
	t.Setenv("PATH", t.TempDir())

	r := &Resolver{BundledDir: dir, logger: log.Nop()}
	ext, err := r.Resolve("manual")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ext.Path != manual {
		t.Fatalf("path = %q, want %q", ext.Path, manual)
	}
	if ext.Argv0 != execBase("lstk-manual") {
		t.Fatalf("argv0 = %q, want the file's own name", ext.Argv0)
	}
}

func execBase(base string) string {
	if goruntime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// A toml key that cannot be an extension command name (spaces, path
// separators, shell metacharacters, a leading hyphen) can never be dispatched:
// lstk would build an argv[0] no binary answers to. A bundle describing one is
// broken, not partially usable, and the rule mirrors the release gate's so a
// toml that passes the gate always loads. TOML quoted keys make such names
// syntactically valid, which is why the check cannot be left to the parser.
func TestLoadBundleInvalidCommandNameIsHardError(t *testing.T) {
	for _, name := range []string{"doc tor", "../doctor", "a;b", "-doctor", "doctor.exe", ""} {
		dir := t.TempDir()
		writeExe(t, dir, BundledBinaryName)
		body := "doctor = \"ok\"\n\"" + name + "\" = \"bad\"\n"
		if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBundle(dir, log.Nop()); err == nil {
			t.Errorf("LoadBundle accepted invalid command name %q", name)
		} else if !strings.Contains(err.Error(), name) && name != "" {
			t.Errorf("error for %q does not name the offending key: %v", name, err)
		}
	}
}

func TestLoadBundleCarriesDescriptions(t *testing.T) {
	dir := t.TempDir()
	writeBundle(t, dir, "doctor")

	bundle, err := LoadBundle(dir, log.Nop())
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if got := bundle.Description("doctor"); got != "Description of doctor" {
		t.Fatalf("Description(doctor) = %q", got)
	}
	if got := bundle.Description("nope"); got != "" {
		t.Fatalf("Description(nope) = %q, want empty", got)
	}
}

// Help shows descriptions for bundled extensions only. List carries them on
// each entry so the caller renders from one read of the descriptions file
// instead of parsing it a second time.
func TestListAttachesDescriptionsToBundledEntries(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	writeBundle(t, dir, "doctor")
	writeExe(t, pathDir, "lstk-hello")
	t.Setenv("PATH", pathDir)

	byName := map[string]Extension{}
	for _, ext := range (&Resolver{BundledDir: dir, logger: log.Nop()}).List() {
		byName[ext.Name] = ext
	}
	if got := byName["doctor"].Description; got != "Description of doctor" {
		t.Fatalf("bundled description = %q", got)
	}
	if got := byName["hello"].Description; got != "" {
		t.Fatalf("PATH extension must be name-only, got description %q", got)
	}
}

// Without a bundle binary, standalone lstk-<name> files in the bundled dir
// still take their descriptions from the file (the pre-bundling shape), while a
// PATH extension stays name-only even when the file happens to describe it.
func TestListStandaloneBundledFileDescribedFromFile(t *testing.T) {
	dir := t.TempDir()
	pathDir := t.TempDir()
	writeExe(t, dir, "lstk-deploy")
	writeExe(t, pathDir, "lstk-hello")
	body := "deploy = \"Deploy to LocalStack\"\nhello = \"Not for PATH extensions\"\n"
	if err := os.WriteFile(filepath.Join(dir, DescriptionsFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	byName := map[string]Extension{}
	for _, ext := range (&Resolver{BundledDir: dir, logger: log.Nop()}).List() {
		byName[ext.Name] = ext
	}
	if got := byName["deploy"].Description; got != "Deploy to LocalStack" {
		t.Fatalf("standalone bundled description = %q", got)
	}
	if got := byName["hello"].Description; got != "" {
		t.Fatalf("PATH extension must be name-only, got description %q", got)
	}
}
