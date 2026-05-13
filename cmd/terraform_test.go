package cmd

import (
	"testing"

	terraformcli "github.com/localstack/lstk/internal/iac/terraform/cli"
	"github.com/stretchr/testify/assert"
)

func TestBuildTerraformEndpointsDefaultsBaseAndDerivesS3(t *testing.T) {
	endpoints := buildTerraformEndpoints(nil, "http://localhost:4566")

	assert.Equal(t, []terraformcli.Endpoint{
		{Service: "", URL: "http://localhost:4566"},
		{Service: "S3", URL: "http://s3.localhost.localstack.cloud:4566"},
	}, endpoints)
}

func TestBuildTerraformEndpointsUserBaseWinsAndDerivesS3FromIt(t *testing.T) {
	env := []string{"AWS_ENDPOINT_URL=https://custom.example:9000"}
	endpoints := buildTerraformEndpoints(env, "http://localhost:4566")

	assert.Equal(t, []terraformcli.Endpoint{
		{Service: "", URL: "https://custom.example:9000"},
		{Service: "S3", URL: "https://s3.custom.example:9000"},
	}, endpoints)
}

func TestBuildTerraformEndpointsUserS3WinsOverDerived(t *testing.T) {
	env := []string{"AWS_ENDPOINT_URL_S3=https://s3-custom:9000"}
	endpoints := buildTerraformEndpoints(env, "http://localhost:4566")

	assert.Equal(t, []terraformcli.Endpoint{
		{Service: "", URL: "http://localhost:4566"},
		{Service: "S3", URL: "https://s3-custom:9000"},
	}, endpoints)
}

func TestBuildTerraformEndpointsForwardsArbitraryServices(t *testing.T) {
	env := []string{
		"AWS_ENDPOINT_URL_LAMBDA=http://customer-lambda:9000",
		"AWS_ENDPOINT_URL_DYNAMODB=http://customer-ddb:9000",
		"PATH=/usr/bin",
	}
	endpoints := buildTerraformEndpoints(env, "http://localhost:4566")

	// Default + S3 come first; the rest are appended alphabetically.
	assert.Equal(t, []terraformcli.Endpoint{
		{Service: "", URL: "http://localhost:4566"},
		{Service: "S3", URL: "http://s3.localhost.localstack.cloud:4566"},
		{Service: "DYNAMODB", URL: "http://customer-ddb:9000"},
		{Service: "LAMBDA", URL: "http://customer-lambda:9000"},
	}, endpoints)
}

func TestBuildTerraformEndpointsIgnoresEmptyValues(t *testing.T) {
	env := []string{
		"AWS_ENDPOINT_URL=",
		"AWS_ENDPOINT_URL_LAMBDA=",
		"AWS_ENDPOINT_URL_S3=",
	}
	endpoints := buildTerraformEndpoints(env, "http://localhost:4566")

	// Empty values are treated as unset → fall back to lstk defaults; LAMBDA
	// has no lstk default, so it isn't present.
	assert.Equal(t, []terraformcli.Endpoint{
		{Service: "", URL: "http://localhost:4566"},
		{Service: "S3", URL: "http://s3.localhost.localstack.cloud:4566"},
	}, endpoints)
}
