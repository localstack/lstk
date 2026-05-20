package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLicense_BadRequest_UnsupportedTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": true, "message": "licensing.license.format:illegal version string adsfgt"}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLicense(context.Background(), &LicenseRequest{})

	require.Error(t, err)
	var licErr *LicenseError
	require.ErrorAs(t, err, &licErr)
	assert.True(t, licErr.IsUnsupportedTag)
}

func TestGetLicense_BadRequest_InvalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": true, "message": "invalid token format"}`))
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLicense(context.Background(), &LicenseRequest{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token format, missing license assignment, or missing subscription")
}

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
