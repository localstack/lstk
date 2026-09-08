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
			_, err := checkVersionString(tt.out)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tt.out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.out, err)
			}
		})
	}
}

// The parsed version is returned so Run can pick the S3 addressing pair
// without a second `cdk --version`.
func TestCheckVersionStringReturnsParsedVersion(t *testing.T) {
	v, err := checkVersionString("2.1138.0 (build 0b2e50a)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Major != 2 || v.Minor != 1138 || v.Patch != 0 {
		t.Fatalf("got %+v, want {2 1138 0}", v)
	}
}

func TestSupportsPathStyleFlag(t *testing.T) {
	tests := []struct {
		version Version
		want    bool
	}{
		{Version{2, 1138, 0}, true},  // the introducing release
		{Version{2, 1137, 0}, false}, // the one before it
		{Version{2, 1140, 0}, true},
		{Version{2, 177, 0}, false}, // the supported floor
		{Version{2, 1138, 1}, true},
		{Version{3, 0, 0}, true},
		{Version{1, 9999, 0}, false},
	}
	for _, tt := range tests {
		if got := tt.version.SupportsPathStyleFlag(); got != tt.want {
			t.Errorf("%+v: got %v, want %v", tt.version, got, tt.want)
		}
	}
}
