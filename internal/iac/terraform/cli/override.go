package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/log"
)

// overrideOptions configures generation of the provider-override file.
type overrideOptions struct {
	workdir         string
	fileName        string
	endpointURL     string // effective LocalStack endpoint (after AWS_ENDPOINT_URL override)
	region          string // resolved deployment region, encoded into blocks that set none
	account         string // resolved access_key (target account id)
	endpointKeys    []string
	includeProvider bool          // emit provider "aws" blocks and remote-state data blocks (false for init's backend-only override)
	backend         *s3Backend    // S3 backend to redirect, or nil when none is declared
	remoteStates    []remoteState // terraform_remote_state (s3) blocks to redirect
	legacy          bool          // emit legacy flat backend endpoint keys instead of the endpoints {} map
	logger          log.Logger
}

var providerBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "provider", LabelNames: []string{"name"}}},
}

var providerAttrSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{{Name: "alias"}, {Name: "region"}},
}

// generateOverride writes the LocalStack override file and returns the paths it
// wrote so the caller can clean them up. Depending on opts it emits any
// combination of: one `provider "aws"` block per discovered alias (when
// includeProvider is set), a full `terraform { backend "s3" {} }` redirection
// (when a backend is present), and regenerated `data "terraform_remote_state"`
// blocks. init uses a backend-only file (includeProvider false) because the
// provider schema does not exist until init has installed the provider.
func generateOverride(opts overrideOptions) ([]string, error) {
	path := filepath.Join(opts.workdir, opts.fileName)
	if err := ensureSafeToWrite(path); err != nil {
		return nil, err
	}

	pathStyle, s3Endpoint := endpoint.S3Addressing(opts.endpointURL)

	var b strings.Builder
	b.WriteString(overrideFileMarker)
	b.WriteString("\n\n")

	if opts.includeProvider {
		for _, p := range discoverAWSProviders(opts.workdir, opts.logger) {
			region := opts.region
			if p.setsRegion {
				region = ""
			}
			writeProviderBlock(&b, providerBlock{
				alias:        p.alias,
				region:       region,
				account:      opts.account,
				endpointURL:  opts.endpointURL,
				s3Endpoint:   s3Endpoint,
				pathStyle:    pathStyle,
				endpointKeys: opts.endpointKeys,
			})
		}
	}

	form := endpointForm{
		endpointURL: opts.endpointURL,
		s3Endpoint:  s3Endpoint,
		pathStyle:   pathStyle,
		legacy:      opts.legacy,
		region:      opts.region,
		account:     opts.account,
	}
	if opts.backend != nil {
		writeBackendBlock(&b, opts.backend, form)
	}
	for _, rs := range opts.remoteStates {
		writeRemoteStateBlock(&b, rs, form)
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return nil, fmt.Errorf("writing override file %s: %w", path, err)
	}
	return []string{path}, nil
}

// ensureSafeToWrite fails if the target file already exists. lstk keeps no
// persistent record of whether it created the file, so an existing one is
// either authored by the user or orphaned by a previous lstk run that was
// interrupted before cleanup. In both cases we refuse to touch it and let the
// user resolve the conflict, so cleanup never deletes a file we did not write.
func ensureSafeToWrite(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking override file %s: %w", path, err)
	}
	return fmt.Errorf("refusing to overwrite existing file %s — remove it or set LSTK_TF_OVERRIDE_FILE_NAME to a different name", path)
}

// discoveredProvider is one `provider "aws"` configuration found by
// discoverAWSProviders.
type discoveredProvider struct {
	alias string // "" for the default, alias-less provider
	// setsRegion is true when a root-level block for this alias sets `region`
	// itself. The override then leaves region out so the user's value stands,
	// as with plain terraform. Only root-level blocks count: an override file
	// merges only with blocks in its own module directory.
	setsRegion bool
}

// discoverAWSProviders returns each `provider "aws"` configuration found in
// the working directory tree's *.tf files, one per alias. It recurses into sub-directories so provider blocks declared in
// nested modules are also represented; this won't catch every layout (e.g.
// remote modules) but is better than scanning only the top level. Hidden
// directories such as .terraform (the provider/module cache) and .git are
// skipped. Files that fail to parse are logged and skipped individually. If no
// aws provider block is found anywhere, it falls back to a single default
// (alias-less) provider.
func discoverAWSProviders(workdir string, logger log.Logger) []discoveredProvider {
	parser := hclparse.NewParser()
	var providers []discoveredProvider
	index := map[string]int{}
	add := func(alias string, setsRegion bool) {
		i, ok := index[alias]
		if !ok {
			i = len(providers)
			index[alias] = i
			providers = append(providers, discoveredProvider{alias: alias})
		}
		providers[i].setsRegion = providers[i].setsRegion || setsRegion
	}

	walkErr := filepath.WalkDir(workdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than aborting the walk
		}
		if d.IsDir() {
			if path != workdir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".tf") {
			return nil
		}
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			logger.Info("terraform: could not parse %s (%v); skipping it for provider discovery", path, diags)
			return nil
		}
		atRoot := filepath.Dir(path) == filepath.Clean(workdir)
		content, _, _ := file.Body.PartialContent(providerBlockSchema)
		for _, block := range content.Blocks {
			if block.Type != "provider" || len(block.Labels) == 0 || block.Labels[0] != "aws" {
				continue
			}
			alias, hasRegion := providerAttrs(block)
			add(alias, atRoot && hasRegion)
		}
		return nil
	})
	if walkErr != nil {
		logger.Info("terraform: error walking %s (%v); generating a single default provider override", workdir, walkErr)
		return []discoveredProvider{{}}
	}

	if len(providers) == 0 {
		return []discoveredProvider{{}}
	}
	return providers
}

// providerAttrs returns the literal `alias` of a provider block ("" if absent
// or not a string literal) and whether it sets `region`, as a literal or any
// expression.
func providerAttrs(block *hcl.Block) (alias string, hasRegion bool) {
	content, _, _ := block.Body.PartialContent(providerAttrSchema)
	_, hasRegion = content.Attributes["region"]
	attr, ok := content.Attributes["alias"]
	if !ok {
		return "", hasRegion
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || v.Type() != cty.String {
		return "", hasRegion
	}
	return v.AsString(), hasRegion
}

type providerBlock struct {
	alias        string
	region       string
	account      string
	endpointURL  string
	s3Endpoint   string
	pathStyle    bool
	endpointKeys []string
}

func writeProviderBlock(b *strings.Builder, p providerBlock) {
	b.WriteString("provider \"aws\" {\n")
	if p.alias != "" {
		fmt.Fprintf(b, "  alias = %q\n", p.alias)
	}
	fmt.Fprintf(b, "  access_key = %q\n", p.account)
	b.WriteString("  secret_key = \"test\"\n")
	if p.region != "" {
		fmt.Fprintf(b, "  region = %q\n", p.region)
	}
	b.WriteString("  skip_credentials_validation = true\n")
	b.WriteString("  skip_metadata_api_check = true\n")
	fmt.Fprintf(b, "  s3_use_path_style = %t\n", p.pathStyle)
	b.WriteString("\n  endpoints {\n")
	for _, key := range p.endpointKeys {
		endpoint := p.endpointURL
		if key == "s3" {
			endpoint = p.s3Endpoint
		}
		fmt.Fprintf(b, "    %s = %q\n", key, endpoint)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n\n")
}

