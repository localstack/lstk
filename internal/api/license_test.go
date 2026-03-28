package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanDisplayName(t *testing.T) {
	tests := []struct {
		licenseType string
		want        string
	}{
		{"hobby", "Hobby"},
		{"pro", "Pro"},
		{"team", "Teams"},
		{"enterprise", "Enterprise"},
		{"trial", "Trial"},
		{"freemium", "Community"},
		{"base", "Starter"},
		{"ultimate", "Ultimate"},
		{"student", "Student"},
		{"unknown_type", "unknown_type"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.licenseType, func(t *testing.T) {
			resp := &LicenseResponse{LicenseType: tc.licenseType}
			assert.Equal(t, tc.want, resp.PlanDisplayName())
		})
	}
}

func TestPlanDisplayNameNilResponse(t *testing.T) {
	var resp *LicenseResponse
	assert.Equal(t, "", resp.PlanDisplayName())
}
