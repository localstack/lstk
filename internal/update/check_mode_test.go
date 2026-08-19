package update

import "testing"

func TestParseCheckMode(t *testing.T) {
	t.Parallel()

	valid := []struct {
		in   string
		want CheckMode
	}{
		{"prompt", CheckModePrompt},
		{"notify", CheckModeNotify},
		{"off", CheckModeOff},
		// The value is typed by hand into a shell or a CI env file, so case and
		// stray whitespace are tolerated.
		{"OFF", CheckModeOff},
		{" off ", CheckModeOff},
		{"Notify", CheckModeNotify},
	}
	for _, tt := range valid {
		t.Run("valid/"+tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCheckMode(tt.in)
			if err != nil {
				t.Fatalf("ParseCheckMode(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseCheckMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	invalid := []string{"", "yes", "1", "true", "false", "disabled", "pro mpt"}
	for _, in := range invalid {
		t.Run("invalid/"+in, func(t *testing.T) {
			t.Parallel()
			_, err := ParseCheckMode(in)
			if err == nil {
				t.Fatalf("ParseCheckMode(%q) succeeded, want an error", in)
			}
		})
	}
}

func TestParseCheckModeErrorMessage(t *testing.T) {
	t.Parallel()

	_, err := ParseCheckMode("yes")
	if err == nil {
		t.Fatal("expected an error")
	}
	const want = `invalid update_check value "yes" (must be one of: prompt, notify, off)`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
