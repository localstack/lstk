// Package must provides minimal generic test assertions that fail the test
// immediately (t.Fatal semantics). In-house replacement for the subset of
// github.com/shoenig/test/must that lstk uses; diffing is delegated to
// go-cmp, which renders readable struct/slice mismatches.
package must

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Eq fails the test if want and got are not deeply equal (go-cmp semantics).
func Eq[A any](t testing.TB, want, got A, opts ...cmp.Option) {
	t.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
}

// True fails the test if ok is false.
func True(t testing.TB, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("expected condition to be true")
	}
}

// NoError fails the test if err is non-nil.
func NoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Nil fails the test if v is non-nil. Typed nils inside interfaces (e.g. a
// nil *T stored in an any) count as nil.
func Nil(t testing.TB, v any) {
	t.Helper()
	if !isNil(v) {
		t.Fatalf("expected nil, got %#v", v)
	}
}

// NotNil fails the test if v is nil (including typed nils, see Nil).
func NotNil(t testing.TB, v any) {
	t.Helper()
	if isNil(v) {
		t.Fatal("expected non-nil value")
	}
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
