package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLatestCatalogVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/license/catalog/aws/version", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"emulator_type": "aws",
			"version":       "4.14.0",
		})
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	version, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.NoError(t, err)
	assert.Equal(t, "4.14.0", version)
}

func TestGetLatestCatalogVersion_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

func TestGetLatestCatalogVersion_EmptyVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"emulator_type": "aws",
			"version":       "",
		})
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete catalog response")
}

func TestGetLatestCatalogVersion_MissingVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"emulator_type": "aws",
		})
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete catalog response")
}

func TestGetLatestCatalogVersion_EmptyEmulatorType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"emulator_type": "",
			"version":       "4.14.0",
		})
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "incomplete catalog response")
}

func TestGetLatestCatalogVersion_MismatchedEmulatorType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"emulator_type": "azure",
			"version":       "4.14.0",
		})
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	_, err := client.GetLatestCatalogVersion(context.Background(), "aws")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected emulator_type")
}

func TestGetLatestCatalogVersion_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// hang until request context is cancelled
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := NewPlatformClient(srv.URL, log.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GetLatestCatalogVersion(ctx, "aws")

	require.Error(t, err)
}

func TestGetLatestCatalogVersion_ServerDown(t *testing.T) {
	// use a URL with no server behind it
	client := NewPlatformClient("http://127.0.0.1:1", log.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := client.GetLatestCatalogVersion(ctx, "aws")

	require.Error(t, err)
}
