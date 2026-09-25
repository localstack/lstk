package output

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unclassifiedErrorEventBaseline is the number of ErrorEvent literals per file
// that set no Code. It is empty: every ErrorEvent sets a Code, which Fail
// carries into result.error_code on lstk_command telemetry and the --json
// envelope renders. A new literal without one fails this test — pick a code
// from error_code.go (ErrInternal when nothing fits) rather than adding a
// baseline entry.
var unclassifiedErrorEventBaseline = map[string]int{}

func TestEveryNewErrorEventSetsACode(t *testing.T) {
	root := filepath.Join("..", "..")
	counts := map[string]int{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isErrorEventType(lit.Type) {
					return true
				}
				for _, elt := range lit.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Code" {
							return true
						}
					}
				}
				counts[filepath.ToSlash(rel)]++
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}

	files := map[string]bool{}
	for f := range counts {
		files[f] = true
	}
	for f := range unclassifiedErrorEventBaseline {
		files[f] = true
	}
	var sorted []string
	for f := range files {
		sorted = append(sorted, f)
	}
	sort.Strings(sorted)
	for _, f := range sorted {
		assert.Equal(t, unclassifiedErrorEventBaseline[f], counts[f], "%s: ErrorEvent literals without a Code (baseline vs now)", f)
	}
}

func isErrorEventType(expr ast.Expr) bool {
	switch tt := expr.(type) {
	case *ast.Ident:
		return tt.Name == "ErrorEvent"
	case *ast.SelectorExpr:
		return tt.Sel.Name == "ErrorEvent"
	}
	return false
}
