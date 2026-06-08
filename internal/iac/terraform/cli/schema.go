package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ErrInitRequired indicates the Terraform AWS provider is not installed —
// typically because `terraform init` has not been run yet. Callers surface its
// message verbatim, so it is kept generic and end-user-friendly (no mention of
// internals like the provider schema).
var ErrInitRequired = errors.New("the Terraform AWS provider is not installed — run `terraform init`, then try again")

// providersSchema is the subset of `terraform providers schema -json` output we
// need: the AWS provider's nested `endpoints` block, whose attribute names are
// the endpoint keys.
type providersSchema struct {
	ProviderSchemas map[string]providerSchema `json:"provider_schemas"`
}

type providerSchema struct {
	Provider struct {
		Block struct {
			BlockTypes map[string]struct {
				Block struct {
					Attributes map[string]json.RawMessage `json:"attributes"`
				} `json:"block"`
			} `json:"block_types"`
		} `json:"block"`
	} `json:"provider"`
}

// lookupAWSProvider finds the AWS provider's schema regardless of which registry
// host reported it (registry.terraform.io for Terraform, registry.opentofu.org
// for OpenTofu, or a private mirror). It matches the stable awsProviderType
// suffix rather than a fixed host key.
func lookupAWSProvider(schema providersSchema) (providerSchema, bool) {
	for key, ps := range schema.ProviderSchemas {
		if key == awsProviderType || strings.HasSuffix(key, "/"+awsProviderType) {
			return ps, true
		}
	}
	return providerSchema{}, false
}

// EndpointKeys returns the AWS service endpoint keys to write into the
// generated `endpoints {}` block, discovered from the installed provider schema
// via `<tfBin> providers schema -json`. It supports the AWS provider 4.0+.
//
// There is no fallback list. If the schema cannot be retrieved (provider not
// installed, e.g. before `terraform init`) it returns ErrInitRequired; if the
// schema is present but exposes no endpoint keys it returns a plain error. In
// both cases the caller must fail rather than generate an override.
func EndpointKeys(ctx context.Context, tfBin, workdir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, tfBin, "providers", "schema", "-json")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		// A non-zero exit overwhelmingly means the providers are not installed
		// yet (no `terraform init`). Surface the actionable init message.
		return nil, ErrInitRequired
	}
	return parseEndpointKeys(out)
}

// parseEndpointKeys extracts the AWS provider's endpoint keys from the JSON
// output of `terraform providers schema -json`. It returns ErrInitRequired when
// the AWS provider is absent (not installed) and a plain error when the schema
// is present but exposes no endpoint keys.
func parseEndpointKeys(jsonOut []byte) ([]string, error) {
	var schema providersSchema
	if err := json.Unmarshal(jsonOut, &schema); err != nil {
		return nil, fmt.Errorf("parsing terraform provider schema: %w", err)
	}

	aws, ok := lookupAWSProvider(schema)
	if !ok {
		return nil, ErrInitRequired
	}

	endpoints, ok := aws.Provider.Block.BlockTypes["endpoints"]
	if !ok || len(endpoints.Block.Attributes) == 0 {
		return nil, errors.New("the Terraform AWS provider schema exposes no endpoint keys")
	}

	keys := make([]string, 0, len(endpoints.Block.Attributes))
	for k := range endpoints.Block.Attributes {
		keys = append(keys, k)
	}
	keys = dedupeAliasKeys(keys)
	sort.Strings(keys)
	return keys, nil
}

// endpointAliasGroups are sets of AWS provider endpoint keys that are aliases
// for the same service and are mutually exclusive: the provider warns ("Invalid
// Attribute Combination — Only one of the following attributes should be set …",
// "this will be an error in a future release") if more than one member of a
// group is set in a single `endpoints {}` block. The `terraform providers
// schema` JSON does not encode this constraint, so the set is maintained here.
//
// It was captured from `terraform plan -json` against AWS provider 6.48 (39
// groups, with a resource present to force provider configuration — the warning
// does not surface on `validate` or on a plan with no resources), unioned with
// simpledb/sdb for older providers. May need refreshing as the provider adds or
// drops aliases. All members of a group are aliases for the same endpoint, so
// keeping any one routes that service to LocalStack; we keep the first listed.
var endpointAliasGroups = [][]string{
	{"amp", "prometheus", "prometheusservice"},
	{"appautoscaling", "applicationautoscaling"},
	{"appintegrations", "appintegrationsservice"},
	{"ce", "costexplorer"},
	{"cloudcontrol", "cloudcontrolapi"},
	{"cloudhsmv2", "cloudhsm"},
	{"cognitoidp", "cognitoidentityprovider"},
	{"configservice", "config"},
	{"cur", "costandusagereportservice"},
	{"databrew", "gluedatabrew"},
	{"deploy", "codedeploy"},
	{"dms", "databasemigration", "databasemigrationservice"},
	{"ds", "directoryservice"},
	{"elasticbeanstalk", "beanstalk"},
	{"elasticsearch", "es", "elasticsearchservice"},
	{"elb", "elasticloadbalancing"},
	{"elbv2", "elasticloadbalancingv2"},
	{"events", "eventbridge", "cloudwatchevents"},
	{"evidently", "cloudwatchevidently"},
	{"grafana", "managedgrafana", "amg"},
	{"inspector2", "inspectorv2"},
	{"kafka", "msk"},
	{"lexmodels", "lexmodelbuilding", "lexmodelbuildingservice", "lex"},
	{"lexv2models", "lexmodelsv2"},
	{"location", "locationservice"},
	{"logs", "cloudwatchlog", "cloudwatchlogs"},
	{"oam", "cloudwatchobservabilityaccessmanager"},
	{"opensearch", "opensearchservice"},
	{"osis", "opensearchingestion"},
	{"rbin", "recyclebin"},
	{"rdsdata", "rdsdataservice"},
	{"redshiftdata", "redshiftdataapiservice"},
	{"resourcegroupstaggingapi", "resourcegroupstagging"},
	{"rum", "cloudwatchrum"},
	{"s3", "s3api"},
	{"serverlessrepo", "serverlessapprepo", "serverlessapplicationrepository"},
	{"servicecatalogappregistry", "appregistry"},
	{"sfn", "stepfunctions"},
	{"simpledb", "sdb"},
	{"transcribe", "transcribeservice"},
}

// dedupeAliasKeys drops mutually-exclusive endpoint-key aliases, keeping the
// first present member of each group in endpointAliasGroups, so the generated
// `endpoints {}` block never sets two aliases for the same service.
func dedupeAliasKeys(keys []string) []string {
	present := make(map[string]bool, len(keys))
	for _, k := range keys {
		present[k] = true
	}
	drop := make(map[string]bool)
	for _, group := range endpointAliasGroups {
		kept := false
		for _, k := range group {
			if !present[k] {
				continue
			}
			if kept {
				drop[k] = true
			} else {
				kept = true
			}
		}
	}
	if len(drop) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys)-len(drop))
	for _, k := range keys {
		if !drop[k] {
			out = append(out, k)
		}
	}
	return out
}
