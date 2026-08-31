package update

import "testing"

func TestValidateMode(t *testing.T) {
	for _, mode := range []string{ModePrompt, ModeNotify, ModeOff} {
		if err := ValidateMode(mode); err != nil {
			t.Errorf("ValidateMode(%q) = %v, want nil", mode, err)
		}
	}
}

func TestValidateModeRejectsUnknownValue(t *testing.T) {
	if err := ValidateMode("bogus"); err == nil {
		t.Error("ValidateMode(\"bogus\") = nil, want error")
	}
}
