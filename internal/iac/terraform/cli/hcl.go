package cli

import (
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// S3BackendConfig captures the configuration of a Terraform S3 state backend
// block as discovered in the user's .tf files. Used by ensureBackendResources
// to pre-create the bucket and lock table on LocalStack before terraform runs.
type S3BackendConfig struct {
	Bucket        string
	Key           string
	Region        string
	DynamoDBTable string
}

// parseS3Backend scans every *.tf file in dir for a
// `terraform { backend "s3" { ... } }` block and returns the first one it
// finds, or nil if none. Files that fail to parse are skipped — users may
// keep partial/work-in-progress configs alongside their main files.
func parseS3Backend(dir string) (*S3BackendConfig, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, err
	}

	parser := hclparse.NewParser()
	for _, file := range matches {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}
		f, diag := parser.ParseHCL(data, file)
		if diag.HasErrors() || f == nil {
			continue
		}
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for _, blk := range body.Blocks {
			if blk.Type != "terraform" {
				continue
			}
			for _, nested := range blk.Body.Blocks {
				if nested.Type == "backend" && len(nested.Labels) == 1 && nested.Labels[0] == "s3" {
					cfg := parseS3BackendBody(nested.Body)
					return &cfg, nil
				}
			}
		}
	}
	return nil, nil
}

func parseS3BackendBody(body *hclsyntax.Body) S3BackendConfig {
	cfg := S3BackendConfig{}
	for name, attr := range body.Attributes {
		val, ok := stringFromExpr(attr.Expr)
		if !ok {
			continue
		}
		switch name {
		case "bucket":
			cfg.Bucket = val
		case "key":
			cfg.Key = val
		case "region":
			cfg.Region = val
		case "dynamodb_table":
			cfg.DynamoDBTable = val
		}
	}
	return cfg
}

// stringFromExpr extracts a literal string value from an HCL expression. We
// intentionally do not evaluate variables, references, or function calls —
// the parser only mirrors literal user input.
func stringFromExpr(expr hcl.Expression) (string, bool) {
	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		if e.Val.Type().FriendlyName() == "string" {
			return e.Val.AsString(), true
		}
	case *hclsyntax.TemplateExpr:
		if e.IsStringLiteral() {
			parts := make([]byte, 0)
			for _, p := range e.Parts {
				if lit, ok := p.(*hclsyntax.LiteralValueExpr); ok && lit.Val.Type().FriendlyName() == "string" {
					parts = append(parts, lit.Val.AsString()...)
				}
			}
			return string(parts), true
		}
	case *hclsyntax.TemplateWrapExpr:
		return stringFromExpr(e.Wrapped)
	}
	return "", false
}
