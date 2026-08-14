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

// testArchive returns the archive Match uses when called from this file
// (snap_test.go) and registers cleanup of the file and the helper registry so
// tests stay independent. These tests are sequential (t.Setenv forbids
// t.Parallel), so resetting the whole registry is safe.
func testArchive(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wd, "__snapshots__", "snap_test.snap")
	t.Cleanup(func() {
		_ = os.Remove(path)
		mu.Lock()
		defer mu.Unlock()
		visited = map[string]map[string]bool{}
		calls = map[string]int{}
	})
	return path
}

// getEntry reads the named entry from the archive, reporting whether it
// exists.
func getEntry(t *testing.T, archive, name string) (string, bool) {
	t.Helper()
	entries, err := readArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	return entryValue(entries, name)
}

// seed writes the given entries into the archive directly.
func seed(t *testing.T, archive string, values map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []entry
	for name, value := range values {
		entries = append(entries, entry{name: name, value: ensureNL(value)})
	}
	if err := os.WriteFile(archive, formatArchive(entries), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchCreatesThenMatches(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeCreate"}
	Match(ft, "hello\nworld\n")
	if ft.failed {
		t.Fatalf("first Match should create, not fail: %q", ft.msg)
	}
	got, ok := getEntry(t, archive, "TestFakeCreate_1")
	if !ok || got != "hello\nworld\n" {
		t.Fatalf("stored entry: %q ok=%v", got, ok)
	}

	// Same test name matches again on a fresh run (reset the call counter).
	mu.Lock()
	delete(calls, archive+"|TestFakeCreate")
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
	archive := testArchive(t)
	seed(t, archive, map[string]string{"TestFakeMismatch_1": "old"})

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
	archive := testArchive(t)
	seed(t, archive, map[string]string{
		"TestFakeUpdate_1": "old",
		"TestOther_1":      "untouched",
	})

	ft := &fakeT{name: "TestFakeUpdate"}
	Match(ft, "new")
	if ft.failed {
		t.Fatalf("update mode should rewrite, not fail: %q", ft.msg)
	}
	if got, ok := getEntry(t, archive, "TestFakeUpdate_1"); !ok || got != "new\n" {
		t.Fatalf("entry not updated: %q ok=%v", got, ok)
	}
	if got, ok := getEntry(t, archive, "TestOther_1"); !ok || got != "untouched\n" {
		t.Fatalf("sibling entry must survive an update: %q ok=%v", got, ok)
	}
}

func TestMatchMissingSnapshotFailsInCI(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeCI"}
	Match(ft, "content")
	if !ft.fatal {
		t.Fatal("missing snapshot in CI must be fatal")
	}
	if _, ok := getEntry(t, archive, "TestFakeCI_1"); ok {
		t.Fatal("snapshot must not be created in CI")
	}
}

func TestMatchCountsCallsWithinOneTest(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeMulti"}
	Match(ft, "first")
	Match(ft, "second")
	if ft.failed {
		t.Fatalf("unexpected failure: %q", ft.msg)
	}
	for name, want := range map[string]string{"TestFakeMulti_1": "first\n", "TestFakeMulti_2": "second\n"} {
		if got, ok := getEntry(t, archive, name); !ok || got != want {
			t.Fatalf("%s: got=%q ok=%v", name, got, ok)
		}
	}
}

func TestMatchJSONMasksAndCanonicalizes(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeJSON"}
	MatchJSON(ft, []byte(`{"zebra":1,"data":{"version":"4.14.1","name":"aws"}}`), "data.version")
	if ft.failed {
		t.Fatalf("unexpected failure: %q", ft.msg)
	}
	got, ok := getEntry(t, archive, "TestFakeJSON_1")
	if !ok {
		t.Fatal("entry missing")
	}
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

func TestMatchJSONFatalPathsWriteNoSnapshot(t *testing.T) {
	t.Setenv("CI", "")
	archive := testArchive(t)

	MatchJSON(&fakeT{name: "TestFakeJSONInvalid"}, []byte(`not json`))
	MatchJSON(&fakeT{name: "TestFakeJSONBadPath"}, []byte(`{"data":{}}`), "data.version")

	for _, name := range []string{"TestFakeJSONInvalid_1", "TestFakeJSONBadPath_1"} {
		if _, ok := getEntry(t, archive, name); ok {
			t.Fatalf("fatal MatchJSON call must not write %s", name)
		}
	}
}

// TestMatchRejectsTerminatorCollision pins the format limitation: a value
// containing a line that is exactly the "---" terminator cannot be stored and
// must fail loudly instead of corrupting the archive.
func TestMatchRejectsTerminatorCollision(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeCollision"}
	Match(ft, "before\n---\nafter")
	if !ft.fatal {
		t.Fatal("terminator collision must be fatal")
	}
	if !strings.Contains(ft.msg, "sanitize") {
		t.Fatalf("collision message should point at sanitizing, got %q", ft.msg)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("colliding value must not be written")
	}
}

// TestMatchTrailingNewlineInvariant pins the terminator contract: "---"
// always sits on its own line, a value and the same value plus one final
// newline are indistinguishable, but additional trailing blank lines are
// preserved and distinguish values.
func TestMatchTrailingNewlineInvariant(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	ft := &fakeT{name: "TestFakeEOF"}
	Match(ft, "line")
	raw, err := os.ReadFile(archive)
	if err != nil || !strings.HasSuffix(string(raw), "line\n---\n") {
		t.Fatalf("terminator should sit on its own line: %q err=%v", raw, err)
	}

	// "line" and "line\n" are the same snapshot; "line\n\n" is not.
	for got, wantMatch := range map[string]bool{"line": true, "line\n": true, "line\n\n": false} {
		mu.Lock()
		delete(calls, archive+"|TestFakeEOF")
		mu.Unlock()
		ft := &fakeT{name: "TestFakeEOF"}
		Match(ft, got)
		if ft.failed == wantMatch {
			t.Fatalf("Match(%q): failed=%v, want match=%v (%q)", got, ft.failed, wantMatch, ft.msg)
		}
	}
}

// TestMatchPreservesInteriorTrailingBlankLines pins that a value ending in
// blank lines keeps them across the store/compare round-trip.
func TestMatchPreservesInteriorTrailingBlankLines(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	value := "top\n\nmiddle\n\n\n"
	ft := &fakeT{name: "TestFakeBlanks"}
	Match(ft, value)
	if got, ok := getEntry(t, archive, "TestFakeBlanks_1"); !ok || got != value {
		t.Fatalf("blank lines not preserved: %q ok=%v", got, ok)
	}

	mu.Lock()
	delete(calls, archive+"|TestFakeBlanks")
	mu.Unlock()
	ft = &fakeT{name: "TestFakeBlanks"}
	Match(ft, value)
	if ft.failed {
		t.Fatalf("re-Match should succeed: %q", ft.msg)
	}
}

func TestCleanEntries(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "some_test.snap")
	seed(t, archive, map[string]string{
		"TestLive_1": "keep",
		"TestGone_1": "stale",
	})
	seen := map[string]bool{"TestLive_1": true}

	// Report mode: counts but keeps entries.
	n, err := cleanEntries(archive, seen, false)
	if err != nil || n != 1 {
		t.Fatalf("report mode: n=%d err=%v", n, err)
	}
	if _, ok := getEntry(t, archive, "TestGone_1"); !ok {
		t.Fatal("report mode must not delete entries")
	}

	// Update mode: drops the stale entry, keeps the live one.
	n, err = cleanEntries(archive, seen, true)
	if err != nil || n != 1 {
		t.Fatalf("update mode: n=%d err=%v", n, err)
	}
	if _, ok := getEntry(t, archive, "TestGone_1"); ok {
		t.Fatal("stale entry should be deleted")
	}
	if got, ok := getEntry(t, archive, "TestLive_1"); !ok || got != "keep\n" {
		t.Fatalf("live entry should survive: %q ok=%v", got, ok)
	}

	// Dropping the last live entry removes the archive entirely.
	n, err = cleanEntries(archive, map[string]bool{}, true)
	if err != nil || n != 1 {
		t.Fatalf("final update: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("empty archive should be removed")
	}
}

func TestCleanOrphanArchives(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live_test.snap")
	orphan := filepath.Join(dir, "gone_test.snap")
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{live, orphan, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	visited = map[string]map[string]bool{live: {"TestX_1": true}}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		visited = map[string]map[string]bool{}
	})

	mu.Lock()
	n, err := cleanOrphanArchives(false)
	mu.Unlock()
	if err != nil || n != 1 {
		t.Fatalf("report mode: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("report mode must not delete")
	}

	mu.Lock()
	n, err = cleanOrphanArchives(true)
	mu.Unlock()
	if err != nil || n != 1 {
		t.Fatalf("update mode: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan archive should be deleted")
	}
	for _, p := range []string{live, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should survive cleanup: %v", p, err)
		}
	}
}

// TestMatchStoresNearTerminatorLines pins that only a line that is exactly
// "---" is unstorable: lines merely containing or resembling the terminator
// (markdown-style rules, indented dashes, inline dashes) round-trip fine.
func TestMatchStoresNearTerminatorLines(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("UPDATE_SNAPS", "")
	archive := testArchive(t)

	value := "----\n--- text\n ---\na---b\n-- almost --\n"
	ft := &fakeT{name: "TestFakeDashes"}
	Match(ft, value)
	if ft.failed {
		t.Fatalf("near-terminator lines should be storable: %q", ft.msg)
	}
	if got, ok := getEntry(t, archive, "TestFakeDashes_1"); !ok || got != value {
		t.Fatalf("round-trip mangled the value: %q ok=%v", got, ok)
	}

	mu.Lock()
	delete(calls, archive+"|TestFakeDashes")
	mu.Unlock()
	ft = &fakeT{name: "TestFakeDashes"}
	Match(ft, value)
	if ft.failed {
		t.Fatalf("re-Match against stored snapshot failed: %q", ft.msg)
	}
}
