package update

import (
	"strings"
	"testing"
)

const (
	testSumA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSumB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestParseChecksums(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr string
	}{
		{
			name:  "valid manifest",
			input: testSumA + "  lstk_0.2.4_darwin_arm64.tar.gz\n" + testSumB + "  lstk_0.2.4_linux_amd64.tar.gz\n",
			want: map[string]string{
				"lstk_0.2.4_darwin_arm64.tar.gz": testSumA,
				"lstk_0.2.4_linux_amd64.tar.gz":  testSumB,
			},
		},
		{
			name:  "crlf and blank lines",
			input: "\r\n" + testSumA + "  lstk_0.2.4_windows_amd64.zip\r\n\r\n",
			want: map[string]string{
				"lstk_0.2.4_windows_amd64.zip": testSumA,
			},
		},
		{
			name:  "binary mode marker",
			input: testSumA + " *lstk_0.2.4_darwin_arm64.tar.gz\n",
			want: map[string]string{
				"lstk_0.2.4_darwin_arm64.tar.gz": testSumA,
			},
		},
		{
			name:  "uppercase digest normalized",
			input: strings.ToUpper(testSumA) + "  lstk_0.2.4_darwin_arm64.tar.gz\n",
			want: map[string]string{
				"lstk_0.2.4_darwin_arm64.tar.gz": testSumA,
			},
		},
		{
			name:    "single field line",
			input:   testSumA + "\n",
			wantErr: "malformed checksum manifest at line 1",
		},
		{
			name:    "three field line",
			input:   testSumA + "  lstk.tar.gz extra\n",
			wantErr: "malformed checksum manifest at line 1",
		},
		{
			name:    "invalid hex digest",
			input:   "zz" + testSumA[2:] + "  lstk.tar.gz\n",
			wantErr: "invalid SHA-256 digest",
		},
		{
			name:    "digest too short",
			input:   testSumA[:40] + "  lstk.tar.gz\n",
			wantErr: "invalid SHA-256 digest",
		},
		{
			name:    "empty manifest",
			input:   "\n\n",
			wantErr: "checksum manifest is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseChecksums(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseChecksums() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChecksums() unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseChecksums() = %v, want %v", got, tt.want)
			}
			for name, sum := range tt.want {
				if got[name] != sum {
					t.Errorf("parseChecksums()[%q] = %q, want %q", name, got[name], sum)
				}
			}
		})
	}
}
