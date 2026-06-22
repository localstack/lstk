package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/localstack/lstk/internal/log"
)

// remoteState holds a parsed `data "terraform_remote_state"` block whose
// backend is "s3". config retains every literal key/value from the user's
// `config = {}` map (reproduced in full, since Terraform replaces — not merges —
// a data block's config from an override file). workspace, when set, is
// preserved verbatim (raw source) so a `terraform.workspace` reference survives.
type remoteState struct {
	name      string
	config    map[string]cty.Value
	workspace string // raw source of the workspace expression, or "" when unset
}

var dataBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "data", LabelNames: []string{"type", "name"}}},
}

var remoteStateAttrSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "backend"},
		{Name: "workspace"},
		{Name: "config"},
	},
}

// parseRemoteStates scans the working-directory *.tf files for
// `data "terraform_remote_state"` blocks whose `backend = "s3"`, returning one
// remoteState per block in deterministic (name) order. Blocks with a non-s3
// backend, or with no/empty/non-literal config, are skipped (left untouched).
func parseRemoteStates(workdir string, logger log.Logger) []remoteState {
	var states []remoteState
	walkTFFiles(workdir, logger, func(file *hcl.File, path string) bool {
		content, _, _ := file.Body.PartialContent(dataBlockSchema)
		for _, block := range content.Blocks {
			if len(block.Labels) < 2 || block.Labels[0] != "terraform_remote_state" {
				continue
			}
			if rs := remoteStateFromBlock(block, file.Bytes, path, logger); rs != nil {
				states = append(states, *rs)
			}
		}
		return true
	})
	sort.Slice(states, func(i, j int) bool { return states[i].name < states[j].name })
	return states
}

func remoteStateFromBlock(block *hcl.Block, src []byte, path string, logger log.Logger) *remoteState {
	content, _, _ := block.Body.PartialContent(remoteStateAttrSchema)

	backendAttr, ok := content.Attributes["backend"]
	if !ok {
		return nil
	}
	bv, d := backendAttr.Expr.Value(nil)
	if d.HasErrors() || bv.Type() != cty.String || bv.AsString() != "s3" {
		return nil
	}

	configAttr, ok := content.Attributes["config"]
	if !ok {
		return nil
	}
	cv, d := configAttr.Expr.Value(nil)
	if d.HasErrors() || !cv.CanIterateElements() {
		logger.Info("terraform: skipping terraform_remote_state %q in %s — config is not a literal map", block.Labels[1], path)
		return nil
	}
	config := map[string]cty.Value{}
	for k, v := range cv.AsValueMap() {
		config[k] = v
	}
	if len(config) == 0 {
		return nil
	}

	rs := &remoteState{name: block.Labels[1], config: config}
	if wsAttr, ok := content.Attributes["workspace"]; ok {
		rs.workspace = strings.TrimSpace(string(wsAttr.Expr.Range().SliceBytes(src)))
	}
	return rs
}

// writeRemoteStateBlock renders a regenerated `data "terraform_remote_state"`
// block: the user's full config map reproduced (minus lstk-managed endpoint
// keys), the version-aware LocalStack endpoints injected, and any workspace
// reference preserved.
func writeRemoteStateBlock(b *strings.Builder, rs remoteState, e endpointForm) {
	fmt.Fprintf(b, "data \"terraform_remote_state\" %q {\n", rs.name)
	b.WriteString("  backend = \"s3\"\n")
	if rs.workspace != "" {
		fmt.Fprintf(b, "  workspace = %s\n", rs.workspace)
	}
	b.WriteString("  config = {\n")
	writeUserAttrs(b, "    ", rs.config)
	_, hasUserRegion := rs.config["region"]
	writeManagedBackendArgs(b, "    ", hasUserRegion, e)
	b.WriteString("  }\n")
	b.WriteString("}\n\n")
}
