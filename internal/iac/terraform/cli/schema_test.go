package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awsSchemaFixture is a trimmed `terraform providers schema -json` output for
// the AWS provider 4.0+ (only the fields parseEndpointKeys reads). The real
// output lists ~150 endpoint keys; a representative few suffice here, including
// the hyphenated cognito-idp key and s3.
const awsSchemaFixture = `{
  "format_version": "1.0",
  "provider_schemas": {
    "registry.terraform.io/hashicorp/aws": {
      "provider": {
        "version": 0,
        "block": {
          "attributes": {
            "region": {"type": "string", "optional": true}
          },
          "block_types": {
            "endpoints": {
              "nesting_mode": "set",
              "block": {
                "attributes": {
                  "s3": {"type": "string", "optional": true},
                  "sqs": {"type": "string", "optional": true},
                  "cognitoidp": {"type": "string", "optional": true},
                  "dynamodb": {"type": "string", "optional": true}
                }
              }
            }
          }
        }
      }
    }
  }
}`

func TestParseEndpointKeysReturnsSortedKeys(t *testing.T) {
	keys, err := parseEndpointKeys([]byte(awsSchemaFixture))
	require.NoError(t, err)
	assert.Equal(t, []string{"cognitoidp", "dynamodb", "s3", "sqs"}, keys)
}

func TestParseEndpointKeysOpenTofuRegistryHost(t *testing.T) {
	// OpenTofu reports the AWS provider under registry.opentofu.org, not
	// registry.terraform.io. parseEndpointKeys must match it by namespace/type
	// regardless of the reporting registry host.
	const tofuSchema = `{
  "format_version": "1.0",
  "provider_schemas": {
    "registry.opentofu.org/hashicorp/aws": {
      "provider": {
        "block": {
          "block_types": {
            "endpoints": {
              "block": {"attributes": {"s3": {"type": "string"}, "sqs": {"type": "string"}}}
            }
          }
        }
      }
    }
  }
}`
	keys, err := parseEndpointKeys([]byte(tofuSchema))
	require.NoError(t, err)
	assert.Equal(t, []string{"s3", "sqs"}, keys)
}

func TestDedupeAliasKeys(t *testing.T) {
	// Multiple members of mutually-exclusive groups, plus standalone keys.
	in := []string{
		"lex", "lexmodelbuilding", "lexmodels", // lex group → keep lexmodels (first listed)
		"s3", "s3api", // → keep s3
		"databrew", "gluedatabrew", // → keep databrew
		"dynamodb", "sqs", // standalone, untouched
	}
	got := dedupeAliasKeys(in)
	assert.ElementsMatch(t, []string{"lexmodels", "s3", "databrew", "dynamodb", "sqs"}, got)
}

func TestDedupeAliasKeysNoConflicts(t *testing.T) {
	// Only one member of any group present → nothing dropped.
	in := []string{"s3", "sqs", "dynamodb", "lexmodels"}
	assert.ElementsMatch(t, in, dedupeAliasKeys(in))
}

func TestParseEndpointKeysMissingProviderReturnsInitRequired(t *testing.T) {
	// Schema retrieved, but the AWS provider is not installed (no terraform init).
	const noAWS = `{"format_version":"1.0","provider_schemas":{}}`
	_, err := parseEndpointKeys([]byte(noAWS))
	assert.ErrorIs(t, err, ErrInitRequired)
}

func TestParseEndpointKeysEmptyEndpointsReturnsError(t *testing.T) {
	const noEndpoints = `{
  "provider_schemas": {
    "registry.terraform.io/hashicorp/aws": {
      "provider": {"block": {"block_types": {}}}
    }
  }
}`
	_, err := parseEndpointKeys([]byte(noEndpoints))
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrInitRequired), "empty endpoints should be a plain error, not init-required")
}

func TestParseEndpointKeysInvalidJSON(t *testing.T) {
	_, err := parseEndpointKeys([]byte("not json"))
	require.Error(t, err)
}
