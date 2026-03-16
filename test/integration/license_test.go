package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseValidationSuccess(t *testing.T) {
	requireDocker(t)
	authToken := env.Require(t, env.AuthToken)

	cleanupLicense()
	t.Cleanup(cleanupLicense)

	validationErrors := make(chan error, 1)

	// Mock platform API that returns success
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/license/request" && r.Method == http.MethodPost {
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			// Validate with safe type assertions
			product, ok := req["product"].(map[string]interface{})
			if !ok || product["name"] != "localstack-pro" {
				validationErrors <- fmt.Errorf("invalid product field")
				http.Error(w, "invalid product", http.StatusBadRequest)
				return
			}

			credentials, ok := req["credentials"].(map[string]interface{})
			if !ok || credentials["token"] != authToken {
				validationErrors <- fmt.Errorf("invalid credentials field")
				http.Error(w, "invalid credentials", http.StatusBadRequest)
				return
			}

			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")

	// Check for validation errors from handler
	select {
	case validationErr := <-validationErrors:
		t.Fatalf("request validation failed: %v", validationErr)
	default:
	}

	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestLicenseValidationFailure(t *testing.T) {
	requireDocker(t)
	cleanupLicense()
	t.Cleanup(cleanupLicense)

	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).With(env.AuthToken, "test-token-for-license-validation"), "start")
	require.Error(t, err, "expected lstk start to fail with forbidden license")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "license validation failed")
	assert.Contains(t, stderr, "invalid, inactive, or expired")

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	assert.Error(t, err, "container should not exist after license failure")
}

func cleanupLicense() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}
