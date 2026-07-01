package extension

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/log"
)

// writeExe writes an executable file named base (with a platform extension on
// Windows) into dir and returns its path.
func writeExe(t *testing.T, dir, base string) string {
	t.Helper()
	name := base
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestNew(t *testing.T) {
	ext := NewExtension("hello", "/usr/bin/lstk-hello", false)
	if ext.Name != "hello" || ext.Path != "/usr/bin/lstk-hello" || ext.Bundled {
		t.Fatalf("unexpected extension: %+v", ext)
	}
}

func TestResolveBundledWinsOverPath(t *testing.T) {
	bundled := t.TempDir()
	pathDir := t.TempDir()
	bundledPath := writeExe(t, bundled, "lstk-deploy")
	writeExe(t, pathDir, "lstk-deploy")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: bundled, logger: log.Nop()}
	ext, err := r.Resolve("deploy")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ext.Bundled {
		t.Fatalf("expected bundled extension to win, got %+v", ext)
	}
	if ext.Path != bundledPath {
		t.Fatalf("expected path %s, got %s", bundledPath, ext.Path)
	}
}

func TestResolveFallsBackToPath(t *testing.T) {
	bundled := t.TempDir()
	pathDir := t.TempDir()
	writeExe(t, pathDir, "lstk-hello")
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: bundled, logger: log.Nop()}
	ext, err := r.Resolve("hello")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ext.Bundled {
		t.Fatalf("expected PATH extension, got bundled %+v", ext)
	}
}

func TestResolveNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := &Resolver{BundledDir: t.TempDir(), logger: log.Nop()}
	if _, err := r.Resolve("doesnotexist"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListDeduplicatesBundledWins(t *testing.T) {
	bundled := t.TempDir()
	pathDir := t.TempDir()
	writeExe(t, bundled, "lstk-deploy")
	writeExe(t, pathDir, "lstk-deploy") // shadowed by bundled
	writeExe(t, pathDir, "lstk-hello")
	// A non-extension file and a non-executable must be ignored.
	if err := os.WriteFile(filepath.Join(pathDir, "unrelated"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	r := &Resolver{BundledDir: bundled, logger: log.Nop()}
	list := r.List()

	if len(list) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %+v", len(list), list)
	}
	// Sorted by name: deploy, hello.
	if list[0].Name != "deploy" || !list[0].Bundled {
		t.Fatalf("expected bundled deploy first, got %+v", list[0])
	}
	if list[1].Name != "hello" || list[1].Bundled {
		t.Fatalf("expected PATH hello second, got %+v", list[1])
	}
}

func TestListIgnoresNonExecutableOnUnix(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("execute-bit semantics are Unix-only")
	}
	pathDir := t.TempDir()
	// lstk-noexec exists but is not executable, so it must not be listed.
	if err := os.WriteFile(filepath.Join(pathDir, "lstk-noexec"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	r := &Resolver{logger: log.Nop()}
	if list := r.List(); len(list) != 0 {
		t.Fatalf("expected no extensions, got %+v", list)
	}
}

func TestBundledDirResolvesThroughSymlink(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("symlink test is Unix-only")
	}
	realDir := t.TempDir()
	realExe := filepath.Join(realDir, "lstk")
	if err := os.WriteFile(realExe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "lstk")
	if err := os.Symlink(realExe, link); err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks(link) must resolve to realExe, whose dir is realDir.
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Dir(resolved); got != realDir {
		// On macOS, TempDir may itself be under a symlinked /var → /private/var.
		realResolved, _ := filepath.EvalSymlinks(realDir)
		if got != realResolved {
			t.Fatalf("expected bundled dir %s (or %s), got %s", realDir, realResolved, got)
		}
	}
}

// decodeContext extracts and JSON-decodes the LSTK_EXT_CONTEXT value from a
// rendered environment, failing the test if it is absent or malformed.
func decodeContext(t *testing.T, env map[string]string) Context {
	t.Helper()
	raw, ok := env[EnvContext]
	if !ok {
		t.Fatalf("%s not set in environment", EnvContext)
	}
	var c Context
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("decode %s: %v (raw: %s)", EnvContext, err, raw)
	}
	return c
}

func mustEnviron(t *testing.T, c Context, base []string) map[string]string {
	t.Helper()
	entries, err := c.Environ(base)
	if err != nil {
		t.Fatalf("Environ: %v", err)
	}
	return envMap(entries)
}

func TestEnvironRendersJSONContext(t *testing.T) {
	env := mustEnviron(t, Context{
		ConfigDir:      "/home/u/.config/lstk",
		AuthToken:      "tok-123",
		NonInteractive: true,
		Emulators: []Emulator{
			{Type: "aws", Endpoint: "http://localhost:4566", Port: "4566"},
			{Type: "snowflake", Endpoint: "http://localhost:4566", Port: "4566"},
		},
	}, nil)

	if env[EnvAPIVersion] != "1" {
		t.Errorf("%s = %q, want \"1\"", EnvAPIVersion, env[EnvAPIVersion])
	}
	c := decodeContext(t, env)
	if c.ConfigDir != "/home/u/.config/lstk" || c.AuthToken != "tok-123" || !c.NonInteractive {
		t.Errorf("decoded scalars wrong: %+v", c)
	}
	if len(c.Emulators) != 2 || c.Emulators[0].Type != "aws" || c.Emulators[1].Type != "snowflake" {
		t.Errorf("emulators wrong: %+v", c.Emulators)
	}
	if c.Emulators[0].Endpoint != "http://localhost:4566" || c.Emulators[0].Port != "4566" {
		t.Errorf("emulator[0] fields wrong: %+v", c.Emulators[0])
	}
}

func TestEnvironOmitsAbsentValues(t *testing.T) {
	env := mustEnviron(t, Context{ConfigDir: "/cfg"}, nil) // no emulator, no token, interactive

	if env[EnvAPIVersion] != "1" {
		t.Error("version must always be set")
	}
	// authToken must be omitted from the JSON (not present as an empty string).
	if strings.Contains(env[EnvContext], "authToken") {
		t.Errorf("authToken must be omitted when unauthenticated, got: %s", env[EnvContext])
	}
	c := decodeContext(t, env)
	if c.ConfigDir != "/cfg" {
		t.Errorf("configDir = %q, want /cfg", c.ConfigDir)
	}
	if c.AuthToken != "" || c.NonInteractive {
		t.Errorf("token/non-interactive should be zero values: %+v", c)
	}
	// emulators is always present and an empty (non-nil) array.
	if c.Emulators == nil || len(c.Emulators) != 0 {
		t.Errorf("emulators must be an empty array when none running, got: %+v", c.Emulators)
	}
}

func TestEnvironSetsOnlyTheTwoContractVariables(t *testing.T) {
	env := mustEnviron(t, Context{ConfigDir: "/cfg", AuthToken: "t"}, nil)
	var prefixed []string
	for k := range env {
		if strings.HasPrefix(k, envPrefix) {
			prefixed = append(prefixed, k)
		}
	}
	if len(prefixed) != 2 {
		t.Errorf("expected exactly LSTK_EXT_API_VERSION and LSTK_EXT_CONTEXT, got %v", prefixed)
	}
}

func TestEnvironInheritsHostEnvAndStripsStaleContract(t *testing.T) {
	base := []string{"HTTP_PROXY=http://proxy:8080", "LSTK_EXT_CONTEXT=stale", "LSTK_EXT_API_VERSION=99"}
	env := mustEnviron(t, Context{ConfigDir: "/cfg"}, base)

	if env["HTTP_PROXY"] != "http://proxy:8080" {
		t.Error("host environment must be inherited")
	}
	// Stale contract values in the inherited env must be replaced, not duplicated.
	if env[EnvAPIVersion] != "1" {
		t.Errorf("stale %s from host env must be overridden, got %q", EnvAPIVersion, env[EnvAPIVersion])
	}
	if env[EnvContext] == "stale" {
		t.Error("stale LSTK_EXT_CONTEXT from host env must be stripped")
	}
}

func TestInvokeForwardsArgsAndPropagatesExitCode(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("shell-script reference extension is Unix-only")
	}
	dir := t.TempDir()
	// Reference extension: echoes args and the JSON context var, exits with the
	// code given by its first argument.
	script := "#!/bin/sh\n" +
		"echo \"args: $*\"\n" +
		"echo \"context: $LSTK_EXT_CONTEXT\"\n" +
		"exit \"$1\"\n"
	path := filepath.Join(dir, "lstk-ref")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ext := NewExtension("ref", path, false)
	err := Invoke(t.Context(), ext, []string{"3", "--flag"}, Context{ConfigDir: "/cfg", AuthToken: "tok"})

	var exitErr *exec.ExitError
	if err == nil || !asExit(err, &exitErr) {
		t.Fatalf("expected exit error, got %v", err)
	}
	if exitErr.ExitCode() != 3 {
		t.Fatalf("expected exit code 3, got %d", exitErr.ExitCode())
	}
}

func envMap(entries []string) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

// asExit unwraps err (including through output.SilentError) into an *exec.ExitError.
func asExit(err error, target **exec.ExitError) bool {
	for err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			*target = ee
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
