package output

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromptsUseIntentConstructors keeps every prompt in the CLI built through
// Confirm, ActionChoice, or Acknowledge.
//
// The point is not tidiness. A raw UserInputRequestEvent literal asks its author
// to pick a rendering (Vertical true or false) at a moment when the question
// they can actually answer is what the prompt IS — and the cheapest answer,
// leaving the field out, silently ships an inline prompt. That is how the
// license re-login prompt ended up with two advertised keys flattened into one
// dimmed hint (DEVX-1045). Naming the intent instead makes the layout a
// consequence rather than a decision.
//
// Test files are exempt: they construct events to drive the UI, not to ask a
// real user anything. test/integration is a separate module and out of scope.
func TestPromptsUseIntentConstructors(t *testing.T) {
	t.Parallel()

	const guidance = "Use output.ActionChoice (a choice between distinct actions, rendered vertically), " +
		"output.Confirm (y/n on an action the user already requested), or " +
		"output.Acknowledge (a single key, no choice). If unsure which, ask the user."

	root := filepath.Join("..", "..")
	// The constructors themselves live here, and they are the one place allowed
	// to build the event by hand.
	selfPkg := filepath.Join(root, "internal", "output")

	fset := token.NewFileSet()
	// "." covers the root package (main.go) without descending into the module's
	// other trees; WalkDir on it would pull in test/integration, a separate
	// module, and every vendored or generated directory below.
	for _, dir := range []string{".", "internal", "cmd"} {
		walk := filepath.WalkDir
		if dir == "." {
			walk = walkDirTopLevel
		}
		err := walk(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == selfPkg {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}

			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isUserInputRequestEvent(lit.Type) {
					return true
				}
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				t.Errorf("%s:%d builds a raw output.UserInputRequestEvent literal.\n%s",
					filepath.ToSlash(rel), fset.Position(lit.Pos()).Line, guidance)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// walkDirTopLevel visits the files directly inside dir, never its subdirectories.
func walkDirTopLevel(dir string, fn fs.WalkDirFunc) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := fn(filepath.Join(dir, entry.Name()), entry, nil); err != nil {
			return err
		}
	}
	return nil
}

// isUserInputRequestEvent matches the type by name on either side of a possible
// package qualifier, so an import alias cannot slip a literal past the guard.
func isUserInputRequestEvent(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "UserInputRequestEvent"
	case *ast.SelectorExpr:
		return t.Sel.Name == "UserInputRequestEvent"
	}
	return false
}
