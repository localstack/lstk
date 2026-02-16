package components

import (
	"strings"
	"testing"
)

func TestHeaderViewIncludesNimboAndVersion(t *testing.T) {
	t.Parallel()

	view := NewHeader("dev").View()
	if !strings.Contains(view, "LocalStack (lstk)") {
		t.Fatalf("header does not include title: %q", view)
	}
	if !strings.Contains(view, "dev") {
		t.Fatalf("header does not include version: %q", view)
	}
	if !strings.Contains(view, "████▖") {
		t.Fatalf("header does not include Nimbo glyphs: %q", view)
	}
}
