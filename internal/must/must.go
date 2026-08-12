// Package must provides minimal generic test assertions that fail the test
// immediately (t.Fatal semantics). In-house replacement for the subset of
// testify that lstk uses; diffing is delegated to go-cmp, which renders
// readable struct/slice mismatches.
//
// Argument order and trailing msgAndArgs follow testify's conventions
// (want/expected before got/actual; an optional format string plus args at
// the end) so call sites migrate mechanically.
package must

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// message renders testify-style trailing msgAndArgs: a lone value is printed
// as-is, a leading string with further args is a Printf format.
func message(msgAndArgs []any) string {
	switch len(msgAndArgs) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%v", msgAndArgs[0])
	default:
		if format, ok := msgAndArgs[0].(string); ok {
			return fmt.Sprintf(format, msgAndArgs[1:]...)
		}
		return fmt.Sprint(msgAndArgs...)
	}
}

func fatalf(t testing.TB, msgAndArgs []any, format string, args ...any) {
	t.Helper()
	s := fmt.Sprintf(format, args...)
	if m := message(msgAndArgs); m != "" {
		s = m + "\n" + s
	}
	t.Fatalf("%s", s)
}

// Eq fails the test if want and got are not deeply equal (go-cmp semantics).
func Eq[A any](t testing.TB, want, got A, msgAndArgs ...any) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		fatalf(t, msgAndArgs, "mismatch (-want +got):\n%s", diff)
	}
}

// NotEq fails the test if want and got are deeply equal (go-cmp semantics).
func NotEq[A any](t testing.TB, want, got A, msgAndArgs ...any) {
	t.Helper()
	if cmp.Diff(want, got) == "" {
		fatalf(t, msgAndArgs, "expected values to differ, both were %#v", got)
	}
}

// True fails the test if ok is false.
func True(t testing.TB, ok bool, msgAndArgs ...any) {
	t.Helper()
	if !ok {
		fatalf(t, msgAndArgs, "expected condition to be true")
	}
}

// False fails the test if ok is true.
func False(t testing.TB, ok bool, msgAndArgs ...any) {
	t.Helper()
	if ok {
		fatalf(t, msgAndArgs, "expected condition to be false")
	}
}

// NoError fails the test if err is non-nil.
func NoError(t testing.TB, err error, msgAndArgs ...any) {
	t.Helper()
	if err != nil {
		fatalf(t, msgAndArgs, "unexpected error: %v", err)
	}
}

// Error fails the test if err is nil.
func Error(t testing.TB, err error, msgAndArgs ...any) {
	t.Helper()
	if err == nil {
		fatalf(t, msgAndArgs, "expected an error, got nil")
	}
}

// ErrorIs fails the test if errors.Is(err, target) is false.
func ErrorIs(t testing.TB, err, target error, msgAndArgs ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		fatalf(t, msgAndArgs, "expected error chain to contain %v, got %v", target, err)
	}
}

// ErrorAs fails the test if errors.As(err, target) is false.
func ErrorAs(t testing.TB, err error, target any, msgAndArgs ...any) {
	t.Helper()
	if !errors.As(err, target) {
		fatalf(t, msgAndArgs, "expected error chain to contain %T, got %v", target, err)
	}
}

// Nil fails the test if v is non-nil. Typed nils inside interfaces (e.g. a
// nil *T stored in an any) count as nil.
func Nil(t testing.TB, v any, msgAndArgs ...any) {
	t.Helper()
	if !isNil(v) {
		fatalf(t, msgAndArgs, "expected nil, got %#v", v)
	}
}

// NotNil fails the test if v is nil (including typed nils, see Nil).
func NotNil(t testing.TB, v any, msgAndArgs ...any) {
	t.Helper()
	if isNil(v) {
		fatalf(t, msgAndArgs, "expected non-nil value")
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

// Contains fails the test unless container (a string, slice, array, or map)
// contains item (substring, element, or key respectively).
func Contains(t testing.TB, container, item any, msgAndArgs ...any) {
	t.Helper()
	if !contains(t, container, item) {
		fatalf(t, msgAndArgs, "%#v\ndoes not contain\n%#v", container, item)
	}
}

// NotContains is the inverse of Contains.
func NotContains(t testing.TB, container, item any, msgAndArgs ...any) {
	t.Helper()
	if contains(t, container, item) {
		fatalf(t, msgAndArgs, "%#v\nunexpectedly contains\n%#v", container, item)
	}
}

func contains(t testing.TB, container, item any) bool {
	t.Helper()
	if s, ok := container.(string); ok {
		sub, ok := item.(string)
		if !ok {
			t.Fatalf("Contains: string container needs a string item, got %T", item)
		}
		return strings.Contains(s, sub)
	}
	rv := reflect.ValueOf(container)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if reflect.DeepEqual(rv.Index(i).Interface(), item) {
				return true
			}
		}
		return false
	case reflect.Map:
		for _, k := range rv.MapKeys() {
			if reflect.DeepEqual(k.Interface(), item) {
				return true
			}
		}
		return false
	}
	t.Fatalf("Contains: unsupported container type %T", container)
	return false
}

// Empty fails the test unless v is empty: nil, a zero-length string/slice/
// map/array/channel, or a zero value.
func Empty(t testing.TB, v any, msgAndArgs ...any) {
	t.Helper()
	if !isEmpty(v) {
		fatalf(t, msgAndArgs, "expected empty, got %#v", v)
	}
}

// NotEmpty is the inverse of Empty.
func NotEmpty(t testing.TB, v any, msgAndArgs ...any) {
	t.Helper()
	if isEmpty(v) {
		fatalf(t, msgAndArgs, "expected non-empty value of type %T", v)
	}
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array, reflect.Chan:
		return rv.Len() == 0
	case reflect.Pointer:
		return rv.IsNil() || isEmpty(rv.Elem().Interface())
	}
	return rv.IsZero()
}

// Len fails the test unless v (a string, slice, array, map, or channel) has
// length n.
func Len(t testing.TB, v any, n int, msgAndArgs ...any) {
	t.Helper()
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array, reflect.Chan:
		if rv.Len() != n {
			fatalf(t, msgAndArgs, "expected length %d, got %d: %#v", n, rv.Len(), v)
		}
	default:
		t.Fatalf("Len: unsupported type %T", v)
	}
}

// InDelta fails the test unless expected and actual (any numeric kinds) are
// within delta of each other.
func InDelta(t testing.TB, expected, actual any, delta float64, msgAndArgs ...any) {
	t.Helper()
	e, ok1 := toFloat(expected)
	a, ok2 := toFloat(actual)
	if !ok1 || !ok2 {
		t.Fatalf("InDelta: non-numeric arguments %T, %T", expected, actual)
	}
	if diff := e - a; diff < -delta || diff > delta {
		fatalf(t, msgAndArgs, "expected %v within %v of %v", actual, delta, expected)
	}
}

func toFloat(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// Regexp fails the test unless s matches the pattern (compiled with
// regexp.MustCompile).
func Regexp(t testing.TB, pattern, s string, msgAndArgs ...any) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		fatalf(t, msgAndArgs, "%q does not match pattern %q", s, pattern)
	}
}

// ElementsMatch fails the test unless want and got contain the same elements
// the same number of times, ignoring order.
//
// ponytail: O(n²) multiset compare via DeepEqual — fine for test-sized lists,
// revisit if a caller ever passes thousands of elements.
func ElementsMatch[A any](t testing.TB, want, got []A, msgAndArgs ...any) {
	t.Helper()
	if len(want) != len(got) {
		fatalf(t, msgAndArgs, "element count differs: want %d, got %d\nwant: %#v\ngot:  %#v", len(want), len(got), want, got)
		return
	}
	used := make([]bool, len(got))
outer:
	for _, w := range want {
		for i, g := range got {
			if !used[i] && reflect.DeepEqual(w, g) {
				used[i] = true
				continue outer
			}
		}
		fatalf(t, msgAndArgs, "missing element %#v\nwant: %#v\ngot:  %#v", w, want, got)
		return
	}
}

// Less fails the test unless a < b.
func Less[A ~int | ~int64 | ~float64 | ~string](t testing.TB, a, b A, msgAndArgs ...any) {
	t.Helper()
	if a >= b {
		fatalf(t, msgAndArgs, "expected %v < %v", a, b)
	}
}

// Greater fails the test unless a > b.
func Greater[A ~int | ~int64 | ~float64 | ~string](t testing.TB, a, b A, msgAndArgs ...any) {
	t.Helper()
	if a <= b {
		fatalf(t, msgAndArgs, "expected %v > %v", a, b)
	}
}

// GreaterOrEqual fails the test unless a >= b.
func GreaterOrEqual[A ~int | ~int64 | ~float64 | ~string](t testing.TB, a, b A, msgAndArgs ...any) {
	t.Helper()
	if a < b {
		fatalf(t, msgAndArgs, "expected %v >= %v", a, b)
	}
}

// Eventually polls cond every tick until it returns true, failing the test
// if it hasn't within waitFor.
func Eventually(t testing.TB, cond func() bool, waitFor, tick time.Duration, msgAndArgs ...any) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			fatalf(t, msgAndArgs, "condition not met within %v", waitFor)
			return
		}
		time.Sleep(tick)
	}
}

// Never polls cond every tick for the full waitFor window, failing the test
// if it ever returns true.
func Never(t testing.TB, cond func() bool, waitFor, tick time.Duration, msgAndArgs ...any) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if cond() {
			fatalf(t, msgAndArgs, "condition unexpectedly became true within %v", waitFor)
			return
		}
		time.Sleep(tick)
	}
}

// FileExists fails the test unless path exists and is not a directory.
func FileExists(t testing.TB, path string, msgAndArgs ...any) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		fatalf(t, msgAndArgs, "expected file %q to exist: %v", path, err)
		return
	}
	if info.IsDir() {
		fatalf(t, msgAndArgs, "expected %q to be a file, it is a directory", path)
	}
}

// NoFileExists fails the test if path exists as a file.
func NoFileExists(t testing.TB, path string, msgAndArgs ...any) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if !info.IsDir() {
		fatalf(t, msgAndArgs, "expected file %q to not exist", path)
	}
}
