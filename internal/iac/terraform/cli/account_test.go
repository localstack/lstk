package cli

import "testing"

func TestDeactivateAccessKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"AKIAIOSFODNN7EXAMPLE", "LKIAIOSFODNN7EXAMPLE"}, // long-term key
		{"ASIAIOSFODNN7EXAMPLE", "LSIAIOSFODNN7EXAMPLE"}, // temporary session key
		{"test", "test"},                 // mock default untouched
		{"111111111111", "111111111111"}, // 12-digit account id untouched
		{"", ""},                         // empty untouched
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := DeactivateAccessKey(tt.in); got != tt.want {
				t.Errorf("DeactivateAccessKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
