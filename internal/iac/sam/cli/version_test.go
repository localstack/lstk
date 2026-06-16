package cli

import "testing"

func TestCheckVersionString(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantErr bool
	}{
		{"exact minimum", "SAM CLI, version 1.95.0", false},
		{"newer patch", "SAM CLI, version 1.95.5", false},
		{"newer minor", "SAM CLI, version 1.151.0", false},
		{"newer major", "SAM CLI, version 2.0.0", false},
		{"older patch boundary", "SAM CLI, version 1.94.99", true},
		{"older minor", "SAM CLI, version 1.90.0", true},
		{"older major", "SAM CLI, version 0.999.0", true},
		{"unparseable", "sam: command behaving oddly", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVersionString(tt.out)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tt.out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.out, err)
			}
		})
	}
}
