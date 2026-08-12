package snap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeT records failures instead of failing the real test. Only the methods
// Match uses are implemented; anything else nil-panics loudly.
type fakeT struct {
	testing.TB
	name   string
	failed bool
	fatal  bool
	msg    string
	logs   []string
}

func (f *fakeT) Helper()      {}
func (f *fakeT) Name() string { return f.name }

func (f *fakeT) Fatal(args ...any) { f.failed, f.fatal = true, true }

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed, f.fatal = true, true
	f.msg = format
}

func (f *fakeT) Errorf(format string, args ...any) {
	f.failed = true
	f.msg = format
}

func (f *fakeT) Logf(format string, args ...any) {
	f.logs = append(f.logs, format)
}

// snapPath returns where Match will store snapshot n for the given fake test
// name, and registers cleanup of the file and the registry entries so tests
// stay independent.
func snapPath(t *testing.T, fakeName string, n string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(callerFile(t)), "__snapshots__")
	path := filepath.Join(dir, fakeName+"_"+n+".snap")
	t.Cleanup(func() {
		_ = os.Remove(path)
		mu.Lock()
		defer mu.Unlock()
		delete(calls, dir+"|"+fakeName)
		if visited[dir] != nil {
			delete(visited[dir], filepath.Base(path))
		}
	})
	return path
}

func callerFile(t *testing.T) string {
	t.Helper()
	// Match resolves the caller's file; in these tests that is snap_test.go,
	// so snapshots land in this package's __snapshots__ directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "snap_test.go")
}

func TestMatchCreatesThenMatches(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	path := snapPath(t, "TestFakeCreate", "1")

	ft := &fakeT{name: "TestFakeCreate"}
	Match(ft, "hello\nworld\n")
	if ft.failed {
		t.Fatalf("first Match should create, not fail: %q", ft.msg)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
	if string(content) != "hello\nworld\n" {
		t.Fatalf("snapshot content %q", content)
	}

	// Same test name matches again on a fresh run (reset the call counter).
	mu.Lock()
	delete(calls, filepath.Dir(path)+"|TestFakeCreate")
	mu.Unlock()
	ft = &fakeT{name: "TestFakeCreate"}
	Match(ft, "hello\nworld\n")
	if ft.failed {
		t.Fatalf("re-Match against stored snapshot failed: %q", ft.msg)
	}
}

func TestMatchFailsOnMismatch(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	path := snapPath(t, "TestFakeMismatch", "1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &fakeT{name: "TestFakeMismatch"}
	Match(ft, "new")
	if !ft.failed || ft.fatal {
		t.Fatalf("mismatch should Errorf: failed=%v fatal=%v", ft.failed, ft.fatal)
	}
	if !strings.Contains(ft.msg, "-want +got") {
		t.Fatalf("mismatch message should carry a diff, got %q", ft.msg)
	}
}

func TestMatchUpdatesOnEnv(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "true")
	path := snapPath(t, "TestFakeUpdate", "1")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	ft := &fakeT{name: "TestFakeUpdate"}
	Match(ft, "new")
	if ft.failed {
		t.Fatalf("update mode should rewrite, not fail: %q", ft.msg)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "new\n" {
		t.Fatalf("snapshot not updated: %q", content)
	}
}

func TestMatchMissingSnapshotFailsInCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("UPDATE_SNAPS", "")
	path := snapPath(t, "TestFakeCI", "1")

	ft := &fakeT{name: "TestFakeCI"}
	Match(ft, "content")
	if !ft.fatal {
		t.Fatal("missing snapshot in CI must be fatal")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("snapshot must not be created in CI")
	}
}

func TestMatchCountsCallsWithinOneTest(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	path1 := snapPath(t, "TestFakeMulti", "1")
	path2 := snapPath(t, "TestFakeMulti", "2")

	ft := &fakeT{name: "TestFakeMulti"}
	Match(ft, "first")
	Match(ft, "second")
	if ft.failed {
		t.Fatalf("unexpected failure: %q", ft.msg)
	}
	for path, want := range map[string]string{path1: "first\n", path2: "second\n"} {
		content, err := os.ReadFile(path)
		if err != nil || string(content) != want {
			t.Fatalf("%s: content=%q err=%v", path, content, err)
		}
	}
}

func TestMatchJSONMasksAndCanonicalizes(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	path := snapPath(t, "TestFakeJSON", "1")

	ft := &fakeT{name: "TestFakeJSON"}
	MatchJSON(ft, []byte(`{"zebra":1,"data":{"version":"4.14.1","name":"aws"}}`), "data.version")
	if ft.failed {
		t.Fatalf("unexpected failure: %q", ft.msg)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, `"version": "<any>"`) {
		t.Fatalf("masked value missing: %s", got)
	}
	if strings.Contains(got, "4.14.1") {
		t.Fatalf("volatile value leaked into snapshot: %s", got)
	}
	if strings.Index(got, `"data"`) > strings.Index(got, `"zebra"`) {
		t.Fatalf("keys not sorted: %s", got)
	}
}

func TestMatchJSONFailsOnMissingMaskPath(t *testing.T) {
	t.Setenv("CI", "")
	ft := &fakeT{name: "TestFakeJSONBadPath"}
	MatchJSON(ft, []byte(`{"data":{}}`), "data.version")
	if !ft.fatal {
		t.Fatal("missing mask path must be fatal")
	}
}

func TestMatchJSONFailsOnInvalidJSON(t *testing.T) {
	t.Setenv("CI", "")
	ft := &fakeT{name: "TestFakeJSONInvalid"}
	MatchJSON(ft, []byte(`not json`))
	if !ft.fatal {
		t.Fatal("invalid JSON must be fatal")
	}
}

func TestReportObsolete(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "TestLive_1.snap")
	stale := filepath.Join(dir, "TestGone_1.snap")
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{live, stale, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{"TestLive_1.snap": true}

	// Report mode: counts but keeps files.
	n, err := reportObsolete(dir, seen, false)
	if err != nil || n != 1 {
		t.Fatalf("report mode: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("report mode must not delete")
	}

	// Update mode: deletes the stale snapshot only.
	n, err = reportObsolete(dir, seen, true)
	if err != nil || n != 1 {
		t.Fatalf("update mode: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale snapshot should be deleted")
	}
	for _, p := range []string{live, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should survive cleanup: %v", p, err)
		}
	}
}

func TestMatchJSONFatalPathsWriteNoSnapshot(t *testing.T) {
	t.Setenv("CI", "")
	dir := filepath.Join(filepath.Dir(callerFile(t)), "__snapshots__")

	MatchJSON(&fakeT{name: "TestFakeJSONInvalid"}, []byte(`not json`))
	MatchJSON(&fakeT{name: "TestFakeJSONBadPath"}, []byte(`{"data":{}}`), "data.version")

	for _, name := range []string{"TestFakeJSONInvalid_1.snap", "TestFakeJSONBadPath_1.snap"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("fatal MatchJSON call must not write %s", name)
		}
	}
}

// TestMatchTrailingNewlineInvariant pins the EOF contract: stored files end
// with exactly one newline regardless of whether the value had one, and a
// value matches its stored snapshot with or without a trailing newline.
func TestMatchTrailingNewlineInvariant(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	path := snapPath(t, "TestFakeEOF", "1")

	ft := &fakeT{name: "TestFakeEOF"}
	Match(ft, "line\n")
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "line\n" {
		t.Fatalf("stored snapshot should end with exactly one newline: %q err=%v", content, err)
	}

	for _, got := range []string{"line", "line\n"} {
		mu.Lock()
		delete(calls, filepath.Dir(path)+"|TestFakeEOF")
		mu.Unlock()
		ft := &fakeT{name: "TestFakeEOF"}
		Match(ft, got)
		if ft.failed {
			t.Fatalf("Match(%q) should match the stored snapshot: %q", got, ft.msg)
		}
	}
}
