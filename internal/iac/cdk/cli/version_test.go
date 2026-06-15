package cli

import "testing"

func TestCheckVersionString(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantErr bool
	}{
		{"exact minimum", "2.177.0 (build abc1234)", false},
		{"newer patch", "2.177.5", false},
		{"newer minor", "2.200.1 (build xyz)", false},
		{"newer major", "3.0.0", false},
		{"older patch boundary", "2.176.99", true},
		{"older minor", "2.100.0", true},
		{"older major", "1.999.0", true},
		{"unparseable", "cdk: command behaving oddly", true},
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
