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
// One file per snapshot (TestName_N.snap, N = call number within the test),
// so there is no snapshot-file format to parse and cleanup is a directory
// listing.
package snap

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test-only helper state: the visited registry must span all tests in the
// package so Clean can tell live snapshots from obsolete ones.
var (
	mu      sync.Mutex
	visited = map[string]map[string]bool{} // snapshot dir -> filename -> seen
	calls   = map[string]int{}             // dir + test name -> Match call count
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Match compares got against the stored snapshot for the calling test,
// creating the snapshot on first local run or rewriting it when
// UPDATE_SNAPS=true.
func Match(t testing.TB, got string) {
	t.Helper()
	if n := flagValue("test.count"); n != "" && n != "1" {
		t.Fatalf("snap: -count > 1 is not supported (snapshot call numbering would repeat)")
	}
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("snap: cannot resolve calling test file")
	}
	dir := filepath.Join(filepath.Dir(file), "__snapshots__")

	mu.Lock()
	key := dir + "|" + t.Name()
	calls[key]++
	name := unsafeChars.ReplaceAllString(t.Name(), "_") + "_" + strconv.Itoa(calls[key]) + ".snap"
	if visited[dir] == nil {
		visited[dir] = map[string]bool{}
	}
	visited[dir][name] = true
	mu.Unlock()

	path := filepath.Join(dir, name)
	want, err := os.ReadFile(path)
	update := os.Getenv("UPDATE_SNAPS") == "true"
	switch {
	case errors.Is(err, os.ErrNotExist):
		if isCI() && !update {
			t.Fatalf("snap: missing snapshot %s (snapshots are never created in CI; run the test locally and commit the file)", path)
			return
		}
		write(t, path, got)
		t.Logf("snap: created %s", path)
	case err != nil:
		t.Fatalf("snap: reading %s: %v", path, err)
	case string(want) != got:
		if update {
			write(t, path, got)
			t.Logf("snap: updated %s", path)
			return
		}
		t.Errorf("snapshot mismatch (-want +got):\n%s\nrun UPDATE_SNAPS=true go test to update %s", cmp.Diff(string(want), got), path)
	}
}

// Clean runs the package's tests and then handles obsolete snapshots: files
// in visited __snapshots__ directories that no Match call used. They are
// deleted when UPDATE_SNAPS=true, otherwise reported as an error (non-zero
// exit) so stale snapshots can't linger unnoticed. Wire it in TestMain:
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
	for dir, seen := range visited {
		if n, err := reportObsolete(dir, seen, update); err != nil {
			fmt.Fprintf(os.Stderr, "snap: cleaning %s: %v\n", dir, err)
			code = 1
		} else if n > 0 && !update {
			code = 1
		}
	}
	return code
}

// reportObsolete deletes (update mode) or reports unvisited .snap files in
// dir, returning how many it found.
//
// ponytail: only directories where at least one Match ran this process are
// examined — deleting a whole test file orphans its snapshots until some
// other test in the same directory runs Match. Acceptable: any later full
// package run flags them.
func reportObsolete(dir string, seen map[string]bool, update bool) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	obsolete := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".snap") || seen[e.Name()] {
			continue
		}
		obsolete++
		path := filepath.Join(dir, e.Name())
		if update {
			if err := os.Remove(path); err != nil {
				return obsolete, err
			}
			fmt.Fprintf(os.Stderr, "snap: removed obsolete snapshot %s\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "snap: obsolete snapshot %s (UPDATE_SNAPS=true go test to remove)\n", path)
		}
	}
	return obsolete, nil
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("snap: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("snap: %v", err)
	}
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
