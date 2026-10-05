// Package archtest enforces the layering described in docs/ARCHITECTURE.md:
// which clical packages may import which, and that only front-ends touch
// terminal/UI libraries. If this test fails, either fix the import or — if
// the layering itself should change — update the table below and the
// architecture doc together.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const module = "github.com/miphe/clical/"

// layers maps each package (relative to the module root) to the clical
// packages it may import directly. Every package with Go code must be listed:
// adding a package means deciding where it sits.
var layers = map[string][]string{
	// Core: calendar logic, no knowledge of any front-end.
	"internal/domain":           {},
	"internal/agenda":           {"internal/domain"},
	"internal/eventtext":        {"internal/domain"},
	"internal/secrets":          {},
	"internal/config":           {},
	"internal/store":            {"internal/domain"},
	"internal/providers/ical":   {"internal/domain", "internal/eventtext"},
	"internal/providers/google": {"internal/domain", "internal/eventtext"},
	"internal/engine":           {"internal/domain", "internal/store"},
	"internal/app": {"internal/domain", "internal/store", "internal/secrets",
		"internal/engine", "internal/providers/ical", "internal/providers/google"},

	// Front-ends: talk to the core only through internal/app (plus the plain
	// data types in domain and agenda).
	"internal/ui": {"internal/app", "internal/agenda", "internal/domain"},
	"cmd/clical":  {"internal/app", "internal/ui", "internal/agenda", "internal/domain", "internal/config"},

	// Tooling.
	"internal/archtest": {},
}

// frontends may use terminal and UI libraries; nothing else may.
var frontends = map[string]bool{"internal/ui": true, "cmd/clical": true}

var uiLibraries = []string{
	"charm.land/",
	"github.com/charmbracelet/",
	"github.com/alecthomas/kong",
	"golang.org/x/term",
	"github.com/rivo/uniseg",
}

// exemptions are individual files allowed extra imports, with the reason.
var exemptions = map[string]struct {
	extra  []string
	reason string
}{}

func TestLayering(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
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
		pkg := filepath.ToSlash(filepath.Dir(rel))
		seen[pkg] = true

		allowed, known := layers[pkg]
		if !known {
			t.Errorf("%s: package %s isn't in archtest's layer table; add it (see docs/ARCHITECTURE.md)", rel, pkg)
			return nil
		}
		allowedSet := map[string]bool{}
		for _, a := range allowed {
			allowedSet[a] = true
		}
		if ex, ok := exemptions[rel]; ok {
			for _, a := range ex.extra {
				allowedSet[a] = true
			}
		}

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if internal, ok := strings.CutPrefix(ip, module); ok {
				if !allowedSet[internal] {
					t.Errorf("%s imports %s, but %s may only import %v (see docs/ARCHITECTURE.md)",
						rel, internal, pkg, sorted(allowed))
				}
				continue
			}
			if frontends[pkg] {
				continue
			}
			for _, lib := range uiLibraries {
				if strings.HasPrefix(ip, lib) {
					t.Errorf("%s imports %s, but only front-ends (internal/ui, cmd/clical) may use terminal/UI libraries",
						rel, ip)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for pkg := range layers {
		if !seen[pkg] {
			if _, err := os.Stat(filepath.Join(root, pkg)); err != nil {
				t.Errorf("layer table lists %s, which no longer exists; remove it", pkg)
			}
		}
	}
	for file := range exemptions {
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			t.Errorf("exemption for %s, which no longer exists; remove it", file)
		}
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
