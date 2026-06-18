package cli

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/localstack/lstk/internal/log"
)

// backendEndpointServices is the fixed set of AWS service endpoints the S3
// backend (and terraform_remote_state) consults. Unlike the AWS provider — whose
// endpoint keys are discovered from the provider schema — the backend exposes a
// small, fixed surface. Only s3/dynamodb are exercised under lstk's forced mock
// credentials; iam/sts/sso are emitted defensively so a stray credential path
// can never reach real AWS. `sso` has no legacy flat key and is omitted there.
var backendEndpointServices = []string{"s3", "dynamodb", "iam", "sts", "sso"}

// managedBackendKeys are arguments lstk always sets itself on the generated
// backend/remote-state blocks (credentials, skip flags, endpoints, path-style,
// region). Any user-provided value for these is dropped from the copied-forward
// config so lstk's LocalStack-targeting values win and real AWS is never
// contacted — mirroring the self-contained provider override.
// region is intentionally not managed: the user's region (the location of their
// state bucket) is preserved verbatim via writeUserAttrs, and a resolved
// fallback is injected only when the user omitted it.
var managedBackendKeys = map[string]bool{
	"access_key":                  true,
	"secret_key":                  true,
	"skip_credentials_validation": true,
	"skip_metadata_api_check":     true,
	"skip_region_validation":      true,
	"skip_requesting_account_id":  true,
	"use_path_style":              true,
	"force_path_style":            true,
	"endpoint":                    true,
	"endpoints":                   true,
	"dynamodb_endpoint":           true,
	"iam_endpoint":                true,
	"sts_endpoint":                true,
}

// s3Backend holds the parsed `terraform { backend "s3" {} }` configuration. attrs
// retains every literal argument the user wrote (for full reproduction in the
// override, since Terraform replaces — not merges — backend blocks). The named
// fields are conveniences extracted from attrs for provisioning decisions.
type s3Backend struct {
	attrs         map[string]cty.Value
	bucket        string
	region        string
	dynamoDBTable string
}

var terraformBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "terraform"}},
}

var backendBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "backend", LabelNames: []string{"type"}}},
}

// HasS3Backend reports whether the working directory declares a
// `terraform { backend "s3" {} }` block. It is what flips `init` onto the
// proxied path (require emulator, generate backend override, provision).
func HasS3Backend(workdir string, logger log.Logger) bool {
	return parseS3Backend(workdir, logger) != nil
}

// parseS3Backend scans the working-directory *.tf files for a
// `terraform { backend "s3" {} }` block and returns its parsed form, or nil if
// no S3 backend is declared. Backend blocks are only valid in the root module,
// but the recursion (matching provider discovery) is harmless. The first S3
// backend found wins.
func parseS3Backend(workdir string, logger log.Logger) *s3Backend {
	var found *s3Backend
	walkTFFiles(workdir, logger, func(file *hcl.File, path string) bool {
		content, _, _ := file.Body.PartialContent(terraformBlockSchema)
		for _, tfBlock := range content.Blocks {
			inner, _, _ := tfBlock.Body.PartialContent(backendBlockSchema)
			for _, backendBlock := range inner.Blocks {
				if len(backendBlock.Labels) == 0 || backendBlock.Labels[0] != "s3" {
					continue
				}
				found = backendFromBody(backendBlock.Body, path, logger)
				return false // stop walking
			}
		}
		return true
	})
	return found
}

// backendFromBody extracts all literal attributes from an `backend "s3"` body.
// Non-literal (computed) attributes and nested blocks (e.g. assume_role, which
// is out of scope) are skipped with a log line; the common attribute-only
// backend is reproduced faithfully.
func backendFromBody(body hcl.Body, path string, logger log.Logger) *s3Backend {
	b := &s3Backend{attrs: map[string]cty.Value{}}
	attrs, diags := body.JustAttributes()
	if diags.HasErrors() {
		logger.Info("terraform: backend \"s3\" in %s has nested blocks or unsupported syntax (%v); copying its literal attributes only", path, diags)
	}
	for name, attr := range attrs {
		v, vd := attr.Expr.Value(nil)
		if vd.HasErrors() {
			logger.Info("terraform: skipping non-literal backend attribute %q in %s", name, path)
			continue
		}
		b.attrs[name] = v
	}
	b.bucket = stringAttr(b.attrs, "bucket")
	b.region = stringAttr(b.attrs, "region")
	b.dynamoDBTable = stringAttr(b.attrs, "dynamodb_table")
	return b
}

func stringAttr(attrs map[string]cty.Value, name string) string {
	if v, ok := attrs[name]; ok && v.Type() == cty.String && !v.IsNull() {
		return v.AsString()
	}
	return ""
}

// writeBackendBlock renders the full `terraform { backend "s3" {} }` override
// section: every literal user argument copied forward, plus lstk's mock
// credentials, region, skip flags, path-style, and the version-aware endpoint
// set. The full block is reproduced (not a partial overlay) because Terraform
// replaces backend blocks from override files wholesale.
func writeBackendBlock(b *strings.Builder, backend *s3Backend, e endpointForm) {
	b.WriteString("terraform {\n")
	b.WriteString("  backend \"s3\" {\n")
	writeUserAttrs(b, "    ", backend.attrs)
	writeManagedBackendArgs(b, "    ", backend.region != "", e)
	b.WriteString("  }\n")
	b.WriteString("}\n\n")
}

// writeManagedBackendArgs emits the arguments lstk always sets on a backend or
// remote-state block: mock credentials, the skip flags that prevent any contact
// with real AWS, the version-aware path-style flag and endpoint set, and a
// resolved region only when the user did not specify one (their region is
// otherwise carried forward verbatim by writeUserAttrs).
func writeManagedBackendArgs(b *strings.Builder, indent string, hasUserRegion bool, e endpointForm) {
	fmt.Fprintf(b, "%saccess_key = %q\n", indent, e.account)
	fmt.Fprintf(b, "%ssecret_key = \"test\"\n", indent)
	if !hasUserRegion {
		fmt.Fprintf(b, "%sregion = %q\n", indent, e.region)
	}
	fmt.Fprintf(b, "%sskip_credentials_validation = true\n", indent)
	fmt.Fprintf(b, "%sskip_metadata_api_check = true\n", indent)
	fmt.Fprintf(b, "%sskip_region_validation = true\n", indent)
	fmt.Fprintf(b, "%sskip_requesting_account_id = true\n", indent)
	writePathStyle(b, indent, e.legacy, e.pathStyle)
	writeBackendEndpoints(b, indent, e)
}

// endpointForm carries everything the version-aware endpoint rendering needs.
type endpointForm struct {
	endpointURL string // bare LocalStack endpoint (used for non-s3 services)
	s3Endpoint  string // s3-specific endpoint (carries the s3. host prefix when virtual-host)
	pathStyle   bool
	legacy      bool   // emit flat *_endpoint keys instead of the endpoints {} map
	region      string // resolved fallback region
	account     string // resolved access_key
}

// writePathStyle emits the path-style argument under its version-appropriate
// name: `use_path_style` for the modern backend, `force_path_style` for legacy.
func writePathStyle(b *strings.Builder, indent string, legacy bool, pathStyle bool) {
	key := "use_path_style"
	if legacy {
		key = "force_path_style"
	}
	fmt.Fprintf(b, "%s%s = %t\n", indent, key, pathStyle)
}

// writeBackendEndpoints emits either the modern `endpoints = { ... }` map or the
// legacy flat `*_endpoint` keys (no sso) at the given indent. The map form is
// identical in shape for the backend block and the remote-state `config` map.
func writeBackendEndpoints(b *strings.Builder, indent string, e endpointForm) {
	endpointFor := func(service string) string {
		if service == "s3" {
			return e.s3Endpoint
		}
		return e.endpointURL
	}
	if e.legacy {
		legacy := []struct{ key, service string }{
			{"endpoint", "s3"},
			{"dynamodb_endpoint", "dynamodb"},
			{"iam_endpoint", "iam"},
			{"sts_endpoint", "sts"},
		}
		for _, l := range legacy {
			fmt.Fprintf(b, "%s%s = %q\n", indent, l.key, endpointFor(l.service))
		}
		return
	}
	fmt.Fprintf(b, "%sendpoints = {\n", indent)
	for _, service := range backendEndpointServices {
		fmt.Fprintf(b, "%s  %s = %q\n", indent, service, endpointFor(service))
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// writeUserAttrs renders the user's literal backend/remote-state arguments
// (those lstk does not manage itself), sorted for deterministic output.
func writeUserAttrs(b *strings.Builder, indent string, attrs map[string]cty.Value) {
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		if managedBackendKeys[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(b, "%s%s = %s\n", indent, name, renderCtyValue(attrs[name]))
	}
}

// renderCtyValue renders a literal HCL value (string, bool, number) as it should
// appear in the generated config. Unsupported/complex values fall back to a
// quoted string of their GoString so output stays valid HCL rather than
// panicking; such values are rare in backend/remote-state config.
func renderCtyValue(v cty.Value) string {
	if v.IsNull() {
		return "null"
	}
	switch v.Type() {
	case cty.String:
		return fmt.Sprintf("%q", v.AsString())
	case cty.Bool:
		if v.True() {
			return "true"
		}
		return "false"
	case cty.Number:
		return v.AsBigFloat().Text('f', -1)
	default:
		return fmt.Sprintf("%q", v.GoString())
	}
}

// walkTFFiles parses each *.tf file under workdir and invokes fn with the parsed
// file. It mirrors discoverAWSAliases' traversal rules: it recurses into
// sub-directories, skips hidden directories such as .terraform and .git, skips
// the generated override file, and skips files that fail to parse (logged). fn
// returns false to stop the walk early.
func walkTFFiles(workdir string, logger log.Logger, fn func(file *hcl.File, path string) bool) {
	parser := hclparse.NewParser()
	overrideName := overrideFileName()
	stop := false
	_ = filepath.WalkDir(workdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || stop {
			if stop {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() {
			if path != workdir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".tf") || d.Name() == overrideName {
			return nil
		}
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			logger.Info("terraform: could not parse %s (%v); skipping it", path, diags)
			return nil
		}
		if !fn(file, path) {
			stop = true
			return filepath.SkipAll
		}
		return nil
	})
}
