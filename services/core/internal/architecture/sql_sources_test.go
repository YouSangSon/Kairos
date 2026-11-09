package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"omni-folio/services/core/internal/storage/ownership"
)

func TestModuleSQLOwnership(t *testing.T) {
	violations, err := inspectSQLSources("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

type parsedSource struct {
	path      string
	file      *ast.File
	constants map[string]ast.Expr
}

func inspectSQLSources(root string) ([]string, error) {
	var sources []parsedSource
	packages := map[string]map[string]ast.Expr{}
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
		// Legacy packages are covered by the original dependency gate until their atomic migration.
		if !strings.HasPrefix(relative, "modules/") && !strings.HasPrefix(relative, "composition/") && !strings.HasPrefix(relative, "storage/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		pkg := filepath.Dir(relative)
		if packages[pkg] == nil {
			packages[pkg] = map[string]ast.Expr{}
		}
		for _, decl := range file.Decls {
			if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.CONST {
				for _, spec := range group.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						if i < len(value.Values) {
							packages[pkg][name.Name] = value.Values[i]
						}
					}
				}
			}
		}
		sources = append(sources, parsedSource{relative, file, packages[pkg]})
		return nil
	})
	if err != nil {
		return nil, err
	}
	var violations []string
	catalogs := map[string][]string{}
	for _, source := range sources {
		imports := map[string]string{}
		for _, imp := range source.file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			alias := filepath.Base(path)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			imports[alias] = path
		}
		statementType := func(expr ast.Expr) bool {
			selector, ok := expr.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			pkg, ok := selector.X.(*ast.Ident)
			return ok && imports[pkg.Name] == internalPrefix+"storage/observation" && selector.Sel.Name == "Statement"
		}
		collect := func(literal *ast.CompositeLit) {
			fields := map[string]ast.Expr{}
			for _, element := range literal.Elts {
				if field, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := field.Key.(*ast.Ident); ok {
						fields[key.Name] = field.Value
					}
				}
			}
			sql, known := constantString(fields["SQL"], source.constants, map[string]bool{})
			if !known {
				violations = append(violations, source.path+" unresolved SQL statement registration")
				return
			}
			objects, hasObjects := fields["Objects"].(*ast.CompositeLit)
			var names []string
			if hasObjects {
				for _, element := range objects.Elts {
					name, known := constantString(element, source.constants, map[string]bool{})
					if !known {
						violations = append(violations, source.path+" unresolved catalog object registration")
						return
					}
					names = append(names, name)
				}
			}
			expected, err := ownership.CatalogArguments(sql)
			if err != nil {
				violations = append(violations, source.path+" "+err.Error())
				return
			}
			indexes, hasIndexes := fields["ObjectArguments"].(*ast.CompositeLit)
			var actual []int
			if hasIndexes {
				for _, element := range indexes.Elts {
					value, ok := element.(*ast.BasicLit)
					if !ok || value.Kind != token.INT {
						violations = append(violations, source.path+" unresolved catalog argument registration")
						return
					}
					index, err := strconv.Atoi(value.Value)
					if err != nil {
						violations = append(violations, source.path+" unresolved catalog argument registration")
						return
					}
					actual = append(actual, index)
				}
			}
			sort.Ints(expected)
			sort.Ints(actual)
			if !slices.Equal(expected, actual) || len(expected) > 0 && len(names) == 0 {
				violations = append(violations, source.path+" catalog argument registration does not cover object predicates")
				return
			}
			owner, layer := modulePath(source.path)
			if owner == "" || layer != "adapters" || !strings.Contains(source.path, "/internal/adapters/sqlite/") {
				violations = append(violations, source.path+" SQL statement registration must be in private sqlite adapter")
				return
			}
			if err := ownership.ValidateSQL(owner, sql, names); err != nil {
				violations = append(violations, source.path+" "+err.Error())
				return
			}
			catalogs[filepath.Dir(source.path)+"\x00"+sql] = names
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if statementType(literal.Type) {
				collect(literal)
			}
			if array, ok := literal.Type.(*ast.ArrayType); ok && statementType(array.Elt) {
				for _, element := range literal.Elts {
					if entry, ok := element.(*ast.CompositeLit); ok {
						collect(entry)
					}
				}
			}
			return true
		})
	}

	for _, source := range sources {
		owner, layer := modulePath(source.path)
		sqlite := owner != "" && layer == "adapters" && strings.Contains(source.path, "/internal/adapters/sqlite/")
		check := func(sql string, called bool) {
			if !called && !looksLikeSQL(sql) {
				return
			}
			if !sqlite {
				violations = append(violations, source.path+" SQL must be in private sqlite adapter")
				return
			}
			if err := checkOwnedSQL(owner, sql, catalogs[filepath.Dir(source.path)+"\x00"+sql]); err != nil {
				violations = append(violations, source.path+" "+err.Error())
			}
		}
		calledMethods := map[*ast.SelectorExpr]bool{}
		ast.Inspect(source.file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					calledMethods[selector] = true
				}
			}
			return true
		})
		ast.Inspect(source.file, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok && !calledMethods[selector] {
				switch selector.Sel.Name {
				case "Query", "QueryRow", "Exec", "Prepare", "QueryContext", "QueryRowContext", "ExecContext", "PrepareContext", "QueryOne", "Execute":
					violations = append(violations, source.path+" SQL method value escapes fixed statement analysis")
				}
			}
			if call, ok := node.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					index := -1
					switch selector.Sel.Name {
					case "Query", "QueryRow", "Exec", "Prepare":
						index = 0
					case "QueryContext", "QueryRowContext", "ExecContext", "PrepareContext", "QueryOne", "Execute":
						index = 1
					}
					if index >= 0 && index < len(call.Args) {
						if approvedSQLForwarder(source.path, source.file, call.Pos()) {
							return true
						}
						sql, known := constantString(call.Args[index], source.constants, map[string]bool{})
						if !known {
							violations = append(violations, source.path+" unresolved SQL expression")
						} else {
							check(sql, true)
						}
					}
				}
			}
			if expr, ok := node.(ast.Expr); ok {
				if sql, known := constantString(expr, source.constants, map[string]bool{}); known && looksLikeSQL(sql) {
					check(sql, false)
					return false
				}
			}
			return true
		})
	}
	return violations, nil
}

func constantString(expr ast.Expr, constants map[string]ast.Expr, seen map[string]bool) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			text, err := strconv.Unquote(value.Value)
			return text, err == nil
		}
	case *ast.ParenExpr:
		return constantString(value.X, constants, seen)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, a := constantString(value.X, constants, seen)
		right, b := constantString(value.Y, constants, seen)
		return left + right, a && b
	case *ast.Ident:
		if seen[value.Name] {
			return "", false
		}
		seen[value.Name] = true
		expression := constants[value.Name]
		if value.Obj != nil {
			if value.Obj.Kind != ast.Con {
				delete(seen, value.Name)
				return "", false
			}
			if declaration, ok := value.Obj.Decl.(*ast.ValueSpec); ok {
				for index, name := range declaration.Names {
					if name.Name == value.Name && index < len(declaration.Values) {
						expression = declaration.Values[index]
					}
				}
			}
		}
		result, ok := constantString(expression, constants, seen)
		delete(seen, value.Name)
		return result, ok
	}
	return "", false
}

func looksLikeSQL(sql string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(sql)))
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "select", "insert", "update", "delete", "replace", "with", "create", "drop", "alter", "pragma", "attach", "detach":
		return true
	}
	return false
}

func inspectSchemaInventory(root string) ([]string, error) {
	var violations []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := checkSchemaSQL(string(data)); err != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", path, err))
		}
		tables, err := ownership.ValidateSchema(string(data))
		if err != nil {
			return nil
		}
		for _, table := range tables {
			seen[table] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	count := 0
	for owner := range moduleEdges {
		for _, table := range ownership.Tables(owner) {
			if table == ownership.ControlTable {
				count++
				continue
			}
			count++
			if !seen[table] {
				violations = append(violations, "registered table missing migration declaration "+table)
			}
		}
	}
	if count != 33 {
		violations = append(violations, fmt.Sprintf("expected 33 canonical tables including schema_migrations, got %d", count))
	}
	return violations, nil
}

func approvedSQLForwarder(path string, file *ast.File, pos token.Pos) bool {
	if path != "storage/observation/statements.go" {
		return false
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv == nil || pos < function.Pos() || pos > function.End() {
			continue
		}
		receiver := function.Recv.List[0].Type
		if pointer, ok := receiver.(*ast.StarExpr); ok {
			receiver = pointer.X
		}
		name, ok := receiver.(*ast.Ident)
		if !ok {
			return false
		}
		return name.Name == "ReadHandle" && (function.Name.Name == "Query" || function.Name.Name == "QueryContext" || function.Name.Name == "QueryOne") || name.Name == "ImportHandle" && function.Name.Name == "Execute"
	}
	return false
}
