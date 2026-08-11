package must

import (
	"errors"
	"strings"
	"testing"
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
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = format
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

func TestTrue(t *testing.T) {
	t.Parallel()

	ft := &fakeT{}
	True(ft, true)
	if ft.failed {
		t.Fatal("True failed on true")
	}
	True(ft, false)
	if !ft.failed {
		t.Fatal("True passed on false")
	}
}

func TestNoError(t *testing.T) {
	t.Parallel()

	ft := &fakeT{}
	NoError(ft, nil)
	if ft.failed {
		t.Fatal("NoError failed on nil error")
	}
	NoError(ft, errors.New("boom"))
	if !ft.failed {
		t.Fatal("NoError passed on non-nil error")
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
