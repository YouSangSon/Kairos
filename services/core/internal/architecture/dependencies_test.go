package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDomainDependencies(t *testing.T) {
	allowed := map[string]map[string]bool{
		"exact": {}, "orderdomain": {}, "paperdomain": {"exact": true},
		"riskdomain": {"exact": true}, "strategydomain": {"exact": true},
		"researchdomain": {"exact": true, "paperdomain": true, "strategydomain": true},
	}
	pure := map[string]bool{"context": true, "encoding/json": true, "errors": true, "fmt": true, "math/big": true, "regexp": true, "strconv": true, "strings": true}
	for domain, dependencies := range allowed {
		t.Run(domain, func(t *testing.T) {
			count := 0
			err := filepath.WalkDir(filepath.Join("..", domain), func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				count++
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imp := range file.Imports {
					name, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						return err
					}
					const prefix = "omni-folio/services/core/internal/"
					if pure[name] || (strings.HasPrefix(name, prefix) && dependencies[strings.TrimPrefix(name, prefix)]) {
						continue
					}
					t.Errorf("%s imports %s: domain must not depend on infrastructure or unapproved domains", path, name)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				t.Fatal("domain has no production source")
			}
		})
	}
}
