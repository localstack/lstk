package styles

import "testing"

func TestNimboColorConstants(t *testing.T) {
	t.Parallel()

	if NimboDarkColor != "#3F51C7" {
		t.Fatalf("unexpected NimboDarkColor: %s", NimboDarkColor)
	}
	if NimboMidColor != "#5E6AD2" {
		t.Fatalf("unexpected NimboMidColor: %s", NimboMidColor)
	}
	if NimboLightColor != "#7E88EC" {
		t.Fatalf("unexpected NimboLightColor: %s", NimboLightColor)
	}
}
