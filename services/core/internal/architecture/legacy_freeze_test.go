package architecture

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed legacy_symbols.json
var legacyBaseline []byte

func TestLegacyRootExemptionsCannotGrow(t *testing.T) {
	var baseline map[string]map[string]string
	if err := json.Unmarshal(legacyBaseline, &baseline); err != nil {
		t.Fatal(err)
	}
	current, err := legacySymbols("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range compareLegacySymbols(baseline, current) {
		t.Error(violation)
	}
}

func compareLegacySymbols(baseline, current map[string]map[string]string) []string {
	var violations []string
	for path, symbols := range current {
		expected, known := baseline[path]
		if !known {
			violations = append(violations, path+" is outside frozen legacy path exemptions")
			continue
		}
		for symbol, hash := range symbols {
			if expected[symbol] != hash {
				violations = append(violations, path+"#"+symbol+" changed frozen legacy symbol; move it to its owner module")
			}
		}
	}
	return violations
}

func legacySymbols(root string) (map[string]map[string]string, error) {
	result := map[string]map[string]string{}
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
		if strings.HasPrefix(relative, "internal/") {
			scope := strings.Split(strings.TrimPrefix(relative, "internal/"), "/")[0]
			if scope == "architecture" || scope == "storage" || scope == "modules" || scope == "composition" || scope == "codec" || scope == "providers" || scope == "transport" || strings.HasPrefix(relative, "internal/files/private/") {
				return nil
			}
		} else if strings.Contains(relative, "/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		if relative == "main.go" && sourceImport(file, "composition") == internalPrefix+"composition" && len(thinMainViolations(file)) == 0 {
			return nil
		}
		result[relative] = map[string]string{}
		for _, declaration := range file.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.IMPORT {
				for _, spec := range group.Specs {
					imp := spec.(*ast.ImportSpec)
					alias := ""
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					var source bytes.Buffer
					if err := format.Node(&source, token.NewFileSet(), imp); err != nil {
						return err
					}
					result[relative]["import."+alias+"."+imp.Path.Value] = fmt.Sprintf("%x", sha256.Sum256(source.Bytes()))
				}
				continue
			}
			symbol := ""
			var node ast.Node = declaration
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				symbol = value.Name.Name
				if value.Recv != nil {
					receiver := value.Recv.List[0].Type
					if pointer, ok := receiver.(*ast.StarExpr); ok {
						receiver = pointer.X
					}
					var name bytes.Buffer
					if err := format.Node(&name, token.NewFileSet(), receiver); err != nil {
						return err
					}
					symbol = name.String() + "." + symbol
				}
			case *ast.GenDecl:
				symbol = value.Tok.String()
				for _, spec := range value.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							symbol += "." + name.Name
						}
					case *ast.TypeSpec:
						symbol += "." + spec.Name.Name
					}
				}
			}
			if _, exists := result[relative][symbol]; exists {
				symbol += fmt.Sprint(declaration.Pos())
			}
			var source bytes.Buffer
			if err := format.Node(&source, token.NewFileSet(), node); err != nil {
				return err
			}
			result[relative][symbol] = fmt.Sprintf("%x", sha256.Sum256(source.Bytes()))
		}
		return nil
	})
	return result, err
}

func TestLegacyFreezeRejectsChangedOrNewSymbols(t *testing.T) {
	baseline := map[string]map[string]string{"legacy.go": {"Read": "old"}}
	if len(compareLegacySymbols(baseline, map[string]map[string]string{"legacy.go": {"Read": "new"}})) == 0 {
		t.Fatal("changed legacy SQL symbol accepted")
	}
	if len(compareLegacySymbols(baseline, map[string]map[string]string{"added.go": {"Read": "old"}})) == 0 {
		t.Fatal("new legacy path accepted")
	}
	if len(compareLegacySymbols(baseline, map[string]map[string]string{"legacy.go": {"Write": "new"}})) == 0 {
		t.Fatal("new legacy symbol accepted")
	}
	if violations := compareLegacySymbols(baseline, map[string]map[string]string{}); len(violations) != 0 {
		t.Fatal("owner migration cannot remove legacy symbols", violations)
	}
}

func TestLegacyFreezeAllowsImportRemoval(t *testing.T) {
	root := fixtureSources(t, map[string]string{"legacy.go": `package legacy; import("fmt"; "errors");func Read(){}`})
	baseline, err := legacySymbols(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.go"), []byte(`package legacy; import "errors";func Read(){}`), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := legacySymbols(root)
	if err != nil {
		t.Fatal(err)
	}
	if violations := compareLegacySymbols(baseline, current); len(violations) != 0 {
		t.Fatal(violations)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.go"), []byte(`package legacy; import "os";func Read(){}`), 0600); err != nil {
		t.Fatal(err)
	}
	current, err = legacySymbols(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(compareLegacySymbols(baseline, current)) == 0 {
		t.Fatal("retargeted import accepted")
	}
}
