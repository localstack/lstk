// Package snap is a minimal file-snapshot testing helper: Match compares a
// string against a snapshot stored under __snapshots__/ next to the calling
// test file, and Clean (wired via TestMain) removes snapshots no test uses
// anymore. In-house replacement for the subset of go-snaps that lstk uses.
//
// Workflow: a missing snapshot is created on the first local run (never in
// CI, where it fails instead). On a mismatch the test fails with a diff;
// UPDATE_SNAPS=true go test ./... rewrites snapshots and lets Clean delete
// obsolete ones.
//
// Storage is one txtar archive per test file (__snapshots__/<test_file>.snap)
// with one section per Match call, named TestName_N (N = call number within
// the test). txtar has no escaping, so a snapshot value containing a line
// that looks like a txtar section separator ("-- name --") cannot be stored;
// write guards against that with a parse round-trip and fails with guidance
// to sanitize the value instead.
package snap

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
)

// header is the comment section of every archive, kept canonical on rewrite.
const header = "Snapshots created by internal/snap. Values are compared with the trailing\nnewline stripped. UPDATE_SNAPS=true go test rewrites this file.\n\n"

// Test-only helper state: the visited registry must span all tests in the
// package so Clean can tell live snapshots from obsolete ones. The mutex also
// serializes read-modify-write cycles on archives shared by parallel tests.
var (
	mu      sync.Mutex
	visited = map[string]map[string]bool{} // archive path -> entry name -> seen
	calls   = map[string]int{}             // archive path + test name -> Match call count
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Match compares got against the stored snapshot for the calling test,
// creating the snapshot on first local run or rewriting it when
// UPDATE_SNAPS=true.
func Match(t testing.TB, got string) {
	t.Helper()
	match(t, got, callerArchive(t))
}

// MatchJSON snapshots got (a JSON document) in canonical pretty-printed form
// (two-space indent, object keys sorted by encoding/json), after replacing
// the values at the given dotted paths (e.g. "data.currentVersion") with the
// placeholder "<any>". Use the paths to mask values that legitimately change
// between runs. A path that doesn't resolve fails the test, so a masked
// field disappearing is caught rather than silently ignored. Paths traverse
// JSON objects only; there is no array-index syntax.
func MatchJSON(t testing.TB, got []byte, maskPaths ...string) {
	t.Helper()
	// The returns after Fatalf matter: tests exercise this path with a fake
	// TB whose Fatalf does not stop the goroutine like *testing.T's does,
	// and a fallthrough would store a bogus snapshot.
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("snap: value is not valid JSON: %v", err)
		return
	}
	for _, path := range maskPaths {
		if !mask(v, strings.Split(path, ".")) {
			t.Fatalf("snap: mask path %q not found in JSON", path)
			return
		}
	}
	// An Encoder rather than MarshalIndent so the "<any>" placeholder isn't
	// HTML-escaped (u003c/u003e) in the stored snapshot. Encode appends the
	// trailing newline.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("snap: %v", err)
	}
	match(t, buf.String(), callerArchive(t))
}

// mask walks nested JSON objects along path segments and replaces the final
// value with "<any>", reporting whether the full path resolved.
func mask(v any, segs []string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if len(segs) == 1 {
		if _, ok := m[segs[0]]; !ok {
			return false
		}
		m[segs[0]] = "<any>"
		return true
	}
	return mask(m[segs[0]], segs[1:])
}

// callerArchive resolves the txtar archive for the test file that called the
// exported Match/MatchJSON function (two frames up):
// <dir>/__snapshots__/<test_file_without_.go>.snap.
func callerArchive(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(2)
	if !ok {
		t.Fatal("snap: cannot resolve calling test file")
	}
	base := strings.TrimSuffix(filepath.Base(file), ".go")
	return filepath.Join(filepath.Dir(file), "__snapshots__", base+".snap")
}

// readArchive parses the archive at path. A missing file is an empty archive.
func readArchive(path string) (*txtar.Archive, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &txtar.Archive{}, nil
	}
	if err != nil {
		return nil, err
	}
	return txtar.Parse(data), nil
}

// entryData returns the entry's value (trailing newline stripped) and whether
// it exists.
func entryData(a *txtar.Archive, name string) (string, bool) {
	for _, f := range a.Files {
		if f.Name == name {
			return strings.TrimSuffix(string(f.Data), "\n"), true
		}
	}
	return "", false
}

func match(t testing.TB, got, archive string) {
	t.Helper()
	if n := flagValue("test.count"); n != "" && n != "1" {
		t.Fatalf("snap: -count > 1 is not supported (snapshot call numbering would repeat)")
	}
	// Values are compared with the trailing newline stripped: the stored form
	// always ends with exactly one newline (txtar sections are line-based), so
	// a snapshot cannot distinguish output ending with a newline from output
	// that doesn't.
	got = strings.TrimSuffix(got, "\n")

	mu.Lock()
	defer mu.Unlock()

	key := archive + "|" + t.Name()
	calls[key]++
	entry := unsafeChars.ReplaceAllString(t.Name(), "_") + "_" + strconv.Itoa(calls[key])
	if visited[archive] == nil {
		visited[archive] = map[string]bool{}
	}
	visited[archive][entry] = true

	a, err := readArchive(archive)
	if err != nil {
		t.Fatalf("snap: reading %s: %v", archive, err)
		return
	}
	want, exists := entryData(a, entry)
	update := os.Getenv("UPDATE_SNAPS") == "true"
	switch {
	case !exists:
		if isCI() && !update {
			t.Fatalf("snap: missing snapshot %q in %s (snapshots are never created in CI; run the test locally and commit the file)", entry, archive)
			return
		}
		writeEntry(t, archive, a, entry, got)
		t.Logf("snap: created %q in %s", entry, archive)
	case want != got:
		if update {
			writeEntry(t, archive, a, entry, got)
			t.Logf("snap: updated %q in %s", entry, archive)
			return
		}
		t.Errorf("snapshot mismatch (-want +got):\n%s\nrun UPDATE_SNAPS=true go test to update %s", cmp.Diff(want, got), archive)
	}
}

// writeEntry upserts entry=value into the archive and saves it with a
// canonical header and name-sorted sections. A parse round-trip guards
// against values txtar cannot represent (a line matching the "-- name --"
// section separator); such values must be sanitized by the caller instead.
// Callers hold mu.
func writeEntry(t testing.TB, path string, a *txtar.Archive, entry, value string) {
	t.Helper()
	data := []byte(value + "\n")
	replaced := false
	for i := range a.Files {
		if a.Files[i].Name == entry {
			a.Files[i].Data = data
			replaced = true
			break
		}
	}
	if !replaced {
		a.Files = append(a.Files, txtar.File{Name: entry, Data: data})
	}
	sort.Slice(a.Files, func(i, j int) bool { return a.Files[i].Name < a.Files[j].Name })
	a.Comment = []byte(header)

	out := txtar.Format(a)
	back := txtar.Parse(out)
	if got, ok := entryData(back, entry); !ok || got != value {
		t.Fatalf("snap: value for %q does not survive the txtar round-trip (it likely contains a line matching the \"-- name --\" section separator); sanitize the value before snapshotting", entry)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("snap: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("snap: %v", err)
	}
}

// Clean runs the package's tests and then handles obsolete snapshots: archive
// entries no Match call used, and whole .snap archives in visited snapshot
// directories that no test touched. They are deleted when UPDATE_SNAPS=true,
// otherwise reported as an error (non-zero exit) so stale snapshots can't
// linger unnoticed. Wire it in TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(snap.Clean(m)) }
//
// Cleanup is skipped on failed or filtered runs (-run/-skip), where "not
// visited" doesn't mean obsolete.
func Clean(m *testing.M) int {
	code := m.Run()
	if code != 0 || flagValue("test.run") != "" || flagValue("test.skip") != "" {
		return code
	}
	update := os.Getenv("UPDATE_SNAPS") == "true"
	mu.Lock()
	defer mu.Unlock()

	for archive, seen := range visited {
		n, err := cleanEntries(archive, seen, update)
		if err != nil {
			fmt.Fprintf(os.Stderr, "snap: cleaning %s: %v\n", archive, err)
			code = 1
		} else if n > 0 && !update {
			code = 1
		}
	}
	if n, err := cleanOrphanArchives(update); err != nil {
		fmt.Fprintf(os.Stderr, "snap: cleaning orphan archives: %v\n", err)
		code = 1
	} else if n > 0 && !update {
		code = 1
	}
	return code
}

// cleanEntries deletes (update mode) or reports entries of archive that no
// Match call visited, returning how many it found. An archive left with no
// entries is removed entirely.
func cleanEntries(archive string, seen map[string]bool, update bool) (int, error) {
	a, err := readArchive(archive)
	if err != nil {
		return 0, err
	}
	var live []txtar.File
	obsolete := 0
	for _, f := range a.Files {
		if seen[f.Name] {
			live = append(live, f)
			continue
		}
		obsolete++
		if update {
			fmt.Fprintf(os.Stderr, "snap: removed obsolete snapshot %q from %s\n", f.Name, archive)
		} else {
			fmt.Fprintf(os.Stderr, "snap: obsolete snapshot %q in %s (UPDATE_SNAPS=true go test to remove)\n", f.Name, archive)
		}
	}
	if obsolete == 0 || !update {
		return obsolete, nil
	}
	if len(live) == 0 {
		return obsolete, os.Remove(archive)
	}
	a.Files = live
	a.Comment = []byte(header)
	return obsolete, os.WriteFile(archive, txtar.Format(a), 0o644)
}

// cleanOrphanArchives deletes (update mode) or reports .snap files in visited
// snapshot directories that no test visited — typically archives of deleted
// or renamed test files.
//
// ponytail: only directories where at least one Match ran this process are
// examined — deleting a whole test file orphans its archive until some other
// test in the same directory runs Match. Acceptable: any later full package
// run flags it.
func cleanOrphanArchives(update bool) (int, error) {
	dirs := map[string]bool{}
	for archive := range visited {
		dirs[filepath.Dir(archive)] = true
	}
	orphans := 0
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return orphans, err
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".snap") || visited[path] != nil {
				continue
			}
			orphans++
			if update {
				if err := os.Remove(path); err != nil {
					return orphans, err
				}
				fmt.Fprintf(os.Stderr, "snap: removed orphan snapshot archive %s\n", path)
			} else {
				fmt.Fprintf(os.Stderr, "snap: orphan snapshot archive %s (UPDATE_SNAPS=true go test to remove)\n", path)
			}
		}
	}
	return orphans, nil
}

func isCI() bool {
	return os.Getenv("CI") != ""
}

func flagValue(name string) string {
	if f := flag.Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}
