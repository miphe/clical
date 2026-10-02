package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The theming rule (docs/ARCHITECTURE.md, internal/ui/theme.go): clical has
// no palette of its own. Every color is one of the terminal's 16 ANSI colors,
// named in exactly one file, so the app follows the terminal's theme.

const themeFile = "internal/ui/theme.go"

var hexColor = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

func TestColorsOnlyFromTheTerminalPalette(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "go-colorful") {
				t.Errorf("%s imports go-colorful; colors must come from the terminal's ANSI palette", rel)
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Color" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "lipgloss" {
					return true
				}
				pos := fset.Position(n.Pos())
				if rel != themeFile {
					t.Errorf("%s:%d names a color; only %s may (use a role from it)", rel, pos.Line, themeFile)
				}
				if !isANSIIndex(n.Args) {
					t.Errorf("%s:%d: lipgloss.Color must be an ANSI index \"0\"–\"15\"", rel, pos.Line)
				}
			case *ast.CompositeLit:
				if sel, ok := n.Type.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "RGBA") || isSel(n.Type, "NRGBA") {
					t.Errorf("%s:%d builds an RGB color; use an ANSI role from %s", rel, fset.Position(n.Pos()).Line, themeFile)
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if v, err := strconv.Unquote(n.Value); err == nil && hexColor.MatchString(v) && strings.HasPrefix(v, "#") {
						t.Errorf("%s:%d has a hex color literal %q", rel, fset.Position(n.Pos()).Line, v)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isANSIIndex(args []ast.Expr) bool {
	if len(args) != 1 {
		return false
	}
	lit, ok := args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(v)
	return err == nil && n >= 0 && n <= 15
}

func isSel(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}
