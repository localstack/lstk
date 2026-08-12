package must

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeT records failures instead of failing the real test. Only the methods
// the assertions use are implemented; anything else nil-panics loudly.
type fakeT struct {
	testing.TB
	failed bool
	msg    string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatal(args ...any) {
	f.failed = true
	f.msg = fmt.Sprint(args...)
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

func TestEq(t *testing.T) {
	t.Parallel()

	ft := &fakeT{}
	Eq(ft, []int{1, 2}, []int{1, 2})
	if ft.failed {
		t.Fatal("Eq failed on equal values")
	}

	ft = &fakeT{}
	Eq(ft, []int{1, 2}, []int{1, 3})
	if !ft.failed {
		t.Fatal("Eq passed on unequal values")
	}
	if !strings.Contains(ft.msg, "-want +got") {
		t.Fatalf("Eq failure should carry a diff, got %q", ft.msg)
	}
}

func TestMessageRendering(t *testing.T) {
	t.Parallel()

	ft := &fakeT{}
	True(ft, false, "context %d", 42)
	if !strings.Contains(ft.msg, "context 42") {
		t.Fatalf("trailing msgAndArgs should be rendered, got %q", ft.msg)
	}

	ft = &fakeT{}
	True(ft, false, "plain message")
	if !strings.Contains(ft.msg, "plain message") {
		t.Fatalf("lone message should be rendered, got %q", ft.msg)
	}
}

func TestNilHandlesTypedNil(t *testing.T) {
	t.Parallel()

	var p *int
	cases := map[string]struct {
		v       any
		wantNil bool
	}{
		"untyped nil":           {nil, true},
		"typed nil pointer":     {p, true},
		"nil slice":             {[]int(nil), true},
		"nil map":               {map[string]int(nil), true},
		"non-nil value":         {42, false},
		"non-nil pointer":       {new(int), false},
		"empty non-nil slice":   {[]int{}, false},
		"zero non-pointer kind": {0, false},
	}
	for name, tc := range cases {
		ft := &fakeT{}
		Nil(ft, tc.v)
		if ft.failed == tc.wantNil {
			t.Errorf("%s: Nil failed=%v, want failure=%v", name, ft.failed, !tc.wantNil)
		}
		ft = &fakeT{}
		NotNil(ft, tc.v)
		if ft.failed != tc.wantNil {
			t.Errorf("%s: NotNil failed=%v, want failure=%v", name, ft.failed, tc.wantNil)
		}
	}
}

// TestAssertions table-drives the pass/fail behavior of the simpler
// assertions: each case runs the assertion against a fakeT and checks
// whether it failed.
func TestAssertions(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sentinel")
	wrapped := fmt.Errorf("outer: %w", sentinel)

	cases := map[string]struct {
		run      func(t testing.TB)
		wantFail bool
	}{
		"True ok":            {func(t testing.TB) { True(t, true) }, false},
		"True bad":           {func(t testing.TB) { True(t, false) }, true},
		"False ok":           {func(t testing.TB) { False(t, false) }, false},
		"False bad":          {func(t testing.TB) { False(t, true) }, true},
		"NoError ok":         {func(t testing.TB) { NoError(t, nil) }, false},
		"NoError bad":        {func(t testing.TB) { NoError(t, sentinel) }, true},
		"Error ok":           {func(t testing.TB) { Error(t, sentinel) }, false},
		"Error bad":          {func(t testing.TB) { Error(t, nil) }, true},
		"ErrorIs ok":         {func(t testing.TB) { ErrorIs(t, wrapped, sentinel) }, false},
		"ErrorIs bad":        {func(t testing.TB) { ErrorIs(t, errors.New("other"), sentinel) }, true},
		"NotEq ok":           {func(t testing.TB) { NotEq(t, 1, 2) }, false},
		"NotEq bad":          {func(t testing.TB) { NotEq(t, 1, 1) }, true},
		"Contains string ok": {func(t testing.TB) { Contains(t, "hello world", "world") }, false},
		"Contains string bad": {func(t testing.TB) {
			Contains(t, "hello world", "mars")
		}, true},
		"Contains slice ok":    {func(t testing.TB) { Contains(t, []string{"a", "b"}, "b") }, false},
		"Contains slice bad":   {func(t testing.TB) { Contains(t, []string{"a", "b"}, "c") }, true},
		"Contains map key ok":  {func(t testing.TB) { Contains(t, map[string]int{"k": 1}, "k") }, false},
		"Contains map key bad": {func(t testing.TB) { Contains(t, map[string]int{"k": 1}, "x") }, true},
		"NotContains ok":       {func(t testing.TB) { NotContains(t, "hello", "mars") }, false},
		"NotContains bad":      {func(t testing.TB) { NotContains(t, "hello", "ell") }, true},
		"Empty string ok":      {func(t testing.TB) { Empty(t, "") }, false},
		"Empty string bad":     {func(t testing.TB) { Empty(t, "x") }, true},
		"Empty slice ok":       {func(t testing.TB) { Empty(t, []int{}) }, false},
		"Empty nil ok":         {func(t testing.TB) { Empty(t, nil) }, false},
		"NotEmpty ok":          {func(t testing.TB) { NotEmpty(t, "x") }, false},
		"NotEmpty bad":         {func(t testing.TB) { NotEmpty(t, "") }, true},
		"Len ok":               {func(t testing.TB) { Len(t, []int{1, 2}, 2) }, false},
		"Len bad":              {func(t testing.TB) { Len(t, []int{1, 2}, 3) }, true},
		"InDelta ok":           {func(t testing.TB) { InDelta(t, 1, 1.0, 0) }, false},
		"InDelta mixed ok":     {func(t testing.TB) { InDelta(t, 252, float64(252), 0) }, false},
		"InDelta bad":          {func(t testing.TB) { InDelta(t, 1, 2, 0.5) }, true},
		"Regexp ok":            {func(t testing.TB) { Regexp(t, `wor\w+`, "hello world") }, false},
		"Regexp bad":           {func(t testing.TB) { Regexp(t, `^world`, "hello world") }, true},
		"ElementsMatch ok": {func(t testing.TB) {
			ElementsMatch(t, []string{"a", "b", "b"}, []string{"b", "a", "b"})
		}, false},
		"ElementsMatch count bad": {func(t testing.TB) {
			ElementsMatch(t, []string{"a"}, []string{"a", "a"})
		}, true},
		"ElementsMatch multiset bad": {func(t testing.TB) {
			ElementsMatch(t, []string{"a", "a"}, []string{"a", "b"})
		}, true},
		"Less ok":            {func(t testing.TB) { Less(t, 1, 2) }, false},
		"Less bad":           {func(t testing.TB) { Less(t, 2, 2) }, true},
		"Greater ok":         {func(t testing.TB) { Greater(t, 2, 1) }, false},
		"Greater bad":        {func(t testing.TB) { Greater(t, 1, 2) }, true},
		"GreaterOrEqual ok":  {func(t testing.TB) { GreaterOrEqual(t, 2, 2) }, false},
		"GreaterOrEqual bad": {func(t testing.TB) { GreaterOrEqual(t, 1, 2) }, true},
	}
	for name, tc := range cases {
		ft := &fakeT{}
		tc.run(ft)
		if ft.failed != tc.wantFail {
			t.Errorf("%s: failed=%v, want %v (msg: %s)", name, ft.failed, tc.wantFail, ft.msg)
		}
	}
}

func TestErrorAs(t *testing.T) {
	t.Parallel()

	pathErr := &os.PathError{Op: "open", Path: "/nope", Err: os.ErrNotExist}
	wrapped := fmt.Errorf("outer: %w", pathErr)

	ft := &fakeT{}
	var target *os.PathError
	ErrorAs(ft, wrapped, &target)
	if ft.failed {
		t.Fatal("ErrorAs failed on matching chain")
	}

	ft = &fakeT{}
	var other *os.LinkError
	ErrorAs(ft, wrapped, &other)
	if !ft.failed {
		t.Fatal("ErrorAs passed on non-matching chain")
	}
}

func TestEventuallyAndNever(t *testing.T) {
	t.Parallel()

	ft := &fakeT{}
	n := 0
	Eventually(ft, func() bool { n++; return n >= 3 }, time.Second, time.Millisecond)
	if ft.failed {
		t.Fatal("Eventually failed on condition that becomes true")
	}

	ft = &fakeT{}
	Eventually(ft, func() bool { return false }, 10*time.Millisecond, time.Millisecond)
	if !ft.failed {
		t.Fatal("Eventually passed on condition that never becomes true")
	}

	ft = &fakeT{}
	Never(ft, func() bool { return false }, 10*time.Millisecond, time.Millisecond)
	if ft.failed {
		t.Fatal("Never failed on condition that stays false")
	}

	ft = &fakeT{}
	Never(ft, func() bool { return true }, time.Second, time.Millisecond)
	if !ft.failed {
		t.Fatal("Never passed on condition that becomes true")
	}
}

func TestFileExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		run      func(t testing.TB)
		wantFail bool
	}{
		"FileExists ok":         {func(t testing.TB) { FileExists(t, file) }, false},
		"FileExists missing":    {func(t testing.TB) { FileExists(t, filepath.Join(dir, "nope")) }, true},
		"FileExists dir":        {func(t testing.TB) { FileExists(t, dir) }, true},
		"NoFileExists ok":       {func(t testing.TB) { NoFileExists(t, filepath.Join(dir, "nope")) }, false},
		"NoFileExists bad":      {func(t testing.TB) { NoFileExists(t, file) }, true},
		"NoFileExists dir okay": {func(t testing.TB) { NoFileExists(t, dir) }, false},
	}
	for name, tc := range cases {
		ft := &fakeT{}
		tc.run(ft)
		if ft.failed != tc.wantFail {
			t.Errorf("%s: failed=%v, want %v", name, ft.failed, tc.wantFail)
		}
	}
}
