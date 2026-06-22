package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseUsesLegacyEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantLegacy bool
	}{
		{"modern terraform", `{"terraform_version":"1.9.5"}`, false},
		{"exactly 1.6 is modern", `{"terraform_version":"1.6.0"}`, false},
		{"modern tofu", `{"terraform_version":"1.8.0","platform":"darwin_arm64"}`, false},
		{"legacy 1.5", `{"terraform_version":"1.5.7"}`, true},
		{"legacy 0.14", `{"terraform_version":"0.14.11"}`, true},
		{"future major", `{"terraform_version":"2.0.0"}`, false},
		{"prerelease minor", `{"terraform_version":"1.6.0-rc1"}`, false},
		{"unparseable json", `not json`, false},
		{"missing field", `{"platform":"linux_amd64"}`, false},
		{"non-numeric version", `{"terraform_version":"dev"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantLegacy, parseUsesLegacyEndpoints([]byte(tt.json)))
		})
	}
}

func TestParseMajorMinor(t *testing.T) {
	cases := []struct {
		in    string
		major int
		minor int
		ok    bool
	}{
		{"1.6.0", 1, 6, true},
		{"1.5.7", 1, 5, true},
		{"1.10.0", 1, 10, true},
		{"1.6", 1, 6, true},
		{"1.6.0-dev", 1, 6, true},
		{"1.6-beta", 1, 6, true},
		{"1", 0, 0, false},
		{"", 0, 0, false},
		{"x.y.z", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			major, minor, ok := parseMajorMinor(c.in)
			assert.Equal(t, c.ok, ok)
			if c.ok {
				assert.Equal(t, c.major, major)
				assert.Equal(t, c.minor, minor)
			}
		})
	}
}
