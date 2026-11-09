package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const internalPrefix = "omni-folio/services/core/internal/"

var domainDependencies = map[string]map[string]bool{
	"exact": {}, "orderdomain": {}, "paperdomain": {"exact": true},
	"riskdomain": {"exact": true}, "strategydomain": {"exact": true},
	"researchdomain": {"exact": true, "paperdomain": true, "strategydomain": true},
	"ledgerdomain":   {"exact": true}, "portfoliodomain": {"exact": true, "ledgerdomain": true},
	"marketdomain": {"exact": true},
}

var pureImports = map[string]bool{
	"context": true, "encoding/json": true, "errors": true, "fmt": true,
	"math/big": true, "regexp": true, "strconv": true, "strings": true,
}

func TestDomainDependencies(t *testing.T) {
	violations, seen, err := inspectDependencies("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
	for _, domain := range []string{"exact", "orderdomain", "paperdomain", "riskdomain", "strategydomain", "researchdomain"} {
		if !seen[domain] {
			t.Errorf("%s has no production source", domain)
		}
	}
}

func inspectDependencies(root string) ([]string, map[string]bool, error) {
	var violations []string
	seen := map[string]bool{}
	methods := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		scope := strings.Split(relative, "/")[0]
		seen[scope] = true
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil {
				continue
			}
			receiver := function.Recv.List[0].Type
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = pointer.X
			}
			name, ok := receiver.(*ast.Ident)
			if !ok {
				continue
			}
			key := filepath.ToSlash(filepath.Dir(relative)) + "/" + name.Name
			if methods[key] == nil {
				methods[key] = map[string]bool{}
			}
			methods[key][function.Name.Name] = true
		}
		imports := map[string]string{}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			alias := filepath.Base(name)
			if imp.Name != nil {
				alias = imp.Name.Name
				if alias == "." {
					violations = append(violations, relative+" uses dot import, hiding dependency ownership")
				}
			}
			imports[alias] = name
			if !allowedImport(scope, name) {
				violations = append(violations, fmt.Sprintf("%s imports forbidden %s", relative, name))
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if _, domain := domainDependencies[scope]; domain && imports[pkg.Name] == "time" {
				switch selector.Sel.Name {
				case "Now", "Since", "Until", "After", "AfterFunc", "NewTimer", "NewTicker", "Tick", "Sleep", "LoadLocation":
					violations = append(violations, relative+" uses impure time."+selector.Sel.Name)
				}
			}
			if imports[pkg.Name] == internalPrefix+"adapters/kiwoom" && selector.Sel.Name == "NewMockDispatch" && scope != "bootstrap" {
				violations = append(violations, relative+" constructs dispatch outside bootstrap")
			}
			return true
		})
		return nil
	})
	for receiver, methodSet := range methods {
		if methodSet["Provider"] && methodSet["Mode"] && methodSet["Preflight"] && methodSet["SubmitLimitOrder"] && !strings.HasPrefix(receiver, "adapters/kiwoom/") {
			violations = append(violations, receiver+" implements production dispatch outside kiwoom adapter")
		}
	}
	return violations, seen, err
}

func allowedImport(scope, name string) bool {
	if dependencies, domain := domainDependencies[scope]; domain {
		return pureImports[name] || ((scope == "ledgerdomain" || scope == "portfoliodomain") && (name == "sort" || name == "time")) || (scope == "portfoliodomain" && name == "math") || (strings.HasPrefix(name, internalPrefix) && dependencies[strings.TrimPrefix(name, internalPrefix)])
	}
	if strings.HasSuffix(scope, "domain") {
		return false
	}
	if scope == "application" {
		if strings.HasPrefix(name, internalPrefix) {
			target := strings.Split(strings.TrimPrefix(name, internalPrefix), "/")[0]
			_, domain := domainDependencies[target]
			return domain || target == "application"
		}
		switch name {
		case "database/sql", "database/sql/driver", "os", "os/exec", "io/fs", "syscall", "unsafe", "plugin", "C":
			return false
		}
		if name == "net" || strings.HasPrefix(name, "net/") || strings.Contains(strings.Split(name, "/")[0], ".") {
			return false
		}
	}
	return true
}

func TestDependencyGateFindsNestedViolations(t *testing.T) {
	cases := []struct{ name, path, source, want string }{
		{"unapproved dispatch implementation", "application/execution/bad.go", "package execution\ntype Fake struct{}\nfunc (Fake) Provider(){}\nfunc (Fake) Mode(){}\nfunc (Fake) Preflight(){}\nfunc (Fake) SubmitLimitOrder(){}", "implements production dispatch"},
		{"domain wall clock", "ledgerdomain/nested/bad.go", "package nested\nimport clock \"time\"\nvar now = clock.Now", "impure time.Now"},
		{"domain SQL", "ledgerdomain/nested/bad.go", "package nested\nimport _ \"database/sql\"", "forbidden database/sql"},
		{"application HTTP alias", "application/ledger/nested/bad.go", "package nested\nimport transport \"net/http\"\nvar _ = transport.MethodGet", "forbidden net/http"},
		{"application adapter", "application/ledger/bad.go", "package ledger\nimport _ \"omni-folio/services/core/internal/adapters/sqlite\"", "forbidden omni-folio/services/core/internal/adapters/sqlite"},
		{"unknown domain", "newdomain/bad.go", "package newdomain\nimport \"fmt\"\nvar _ = fmt.Sprint", "forbidden fmt"},
		{"dispatch dot import", "adapters/cli/bad.go", "package cli\nimport . \"omni-folio/services/core/internal/adapters/kiwoom\"\nvar factory = NewMockDispatch", "dot import"},
		{"dispatch alias reference", "adapters/cli/bad.go", "package cli\nimport broker \"omni-folio/services/core/internal/adapters/kiwoom\"\nvar factory = broker.NewMockDispatch", "outside bootstrap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(tc.path))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			violations, _, err := inspectDependencies(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(violations, "\n"), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, violations)
			}
		})
	}
}

func TestDependencyGateAllowsInwardComposition(t *testing.T) {
	root := t.TempDir()
	sources := map[string]string{
		"application/ledger/commands.go": "package ledger\nimport \"omni-folio/services/core/internal/ledgerdomain\"\nvar _ = ledgerdomain.Command{}",
		"bootstrap/core.go":              "package bootstrap\nimport broker \"omni-folio/services/core/internal/adapters/kiwoom\"\nvar factory = broker.NewMockDispatch",
	}
	for name, source := range sources {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	violations, _, err := inspectDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("inward composition rejected: %v", violations)
	}
}
