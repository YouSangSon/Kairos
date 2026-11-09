package architecture

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

var moduleEdges = map[string]map[string]bool{
	"ledger":      {},
	"marketdata":  {"ledger": true},
	"watchlist":   {"marketdata": true},
	"trading":     {"marketdata": true},
	"portfolio":   {"ledger": true, "marketdata": true},
	"maintenance": {"ledger": true, "marketdata": true, "watchlist": true, "trading": true, "portfolio": true},
}

func modulePath(path string) (owner, layer string) {
	parts := strings.Split(strings.TrimPrefix(path, internalPrefix), "/")
	if len(parts) < 2 || parts[0] != "modules" {
		return "", ""
	}
	owner = parts[1]
	if len(parts) > 2 && parts[2] == "contracts" {
		return owner, "contracts"
	}
	if len(parts) > 3 && parts[2] == "internal" {
		return owner, parts[3]
	}
	return owner, "root"
}

func moduleLayer(path string) string { _, layer := modulePath(path); return layer }

func pureModuleImport(name string) bool {
	if pureImports[name] {
		return true
	}
	switch name {
	case "time", "sort", "math", "slices", "cmp", "unicode", "unicode/utf8", "bytes", "encoding/hex", "crypto/sha256", internalPrefix + "exact":
		return true
	}
	return false
}

func moduleImportViolation(relative, name string) string {
	owner, layer := modulePath(relative)
	target, targetLayer := modulePath(name)
	if owner != "" {
		if _, ok := moduleEdges[owner]; !ok {
			return "unknown module " + owner
		}
	}
	if target != "" {
		if _, ok := moduleEdges[target]; !ok {
			return "unknown module " + target
		}
		if targetLayer != "contracts" && targetLayer != "root" && (owner != target || layer == "contracts") {
			return "imports another module private implementation " + name
		}
		if targetLayer == "root" && !strings.HasPrefix(relative, "composition/") {
			return "layer imports module construction root " + name
		}
		if target != owner && owner != "" {
			if targetLayer != "contracts" {
				return "imports another module private implementation " + name
			}
			if !moduleEdges[owner][target] {
				return "forbidden module dependency " + owner + " -> " + target
			}
		}
	}
	if owner == "" {
		if strings.HasPrefix(relative, "composition/") && strings.HasPrefix(name, internalPrefix) && target == "" && !strings.HasPrefix(name, internalPrefix+"storage/") && !strings.HasPrefix(name, internalPrefix+"composition/") {
			return "composition imports private legacy implementation " + name
		}
		return ""
	}
	switch layer {
	case "domain":
		if target == owner && targetLayer == "domain" {
			return ""
		}
		if strings.HasPrefix(name, internalPrefix) && name != internalPrefix+"exact" {
			return "domain layer imports forbidden " + name
		}
		if !pureModuleImport(name) {
			return "domain layer imports forbidden " + name
		}
	case "application":
		if target != "" && (targetLayer == "contracts" || target == owner && (targetLayer == "domain" || targetLayer == "application")) {
			return ""
		}
		if !pureModuleImport(name) {
			return "application layer imports forbidden " + name
		}
	case "contracts":
		if targetLayer == "contracts" {
			return ""
		}
		if name == internalPrefix+"storage/contracts" {
			return ""
		}
		if !pureModuleImport(name) || name == "regexp" {
			return "public contract imports forbidden " + name
		}
	case "root":
		if filepath.Base(relative) != "compose.go" {
			return "module construction must be in compose.go"
		}
	case "adapters":
		if strings.HasPrefix(name, internalPrefix) && target == "" && name != internalPrefix+"exact" && !strings.HasPrefix(name, internalPrefix+"storage/") {
			return "adapter imports legacy implementation outside module boundary " + name
		}
	default:
		return "unknown module layer " + layer
	}
	return ""
}

func approvedDispatchFactory(path string, file *ast.File, pos token.Pos) bool {
	if strings.HasPrefix(path, "bootstrap/") {
		return true
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || pos < function.Pos() || pos > function.End() {
			continue
		}
		if strings.HasPrefix(path, "modules/trading/internal/adapters/kiwoom/") && strings.HasPrefix(function.Name.Name, "New") {
			return true
		}
		return path == "modules/trading/compose.go" && function.Name.Name == "Compose"
	}
	return false
}

func inspectModuleDeclarations(path string, file *ast.File, imports map[string]string) []string {
	owner, layer := modulePath(path)
	if owner == "" {
		return nil
	}
	var violations []string
	if _, ok := moduleEdges[owner]; !ok {
		violations = append(violations, path+" unknown module "+owner)
	}
	if layer == "root" {
		if filepath.Base(path) != "compose.go" {
			violations = append(violations, path+" module root is construction only")
		}
		for _, declaration := range file.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.VAR {
				violations = append(violations, path+" module construction cannot expose package state")
			}
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if function.Name.Name != "Compose" || function.Recv != nil {
				violations = append(violations, path+" module root permits Compose construction only")
				continue
			}
			if function.Type.Results != nil {
				for _, field := range function.Type.Results.List {
					if forbiddenContractResult(field.Type, imports) {
						violations = append(violations, path+" construction result exposes private capability")
					}
				}
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if condition, ok := node.(*ast.IfStmt); ok {
					binary, valid := condition.Cond.(*ast.BinaryExpr)
					if !valid || binary.Op != token.NEQ || !isNil(binary.Y) || !isErrorIdentifier(binary.X) {
						violations = append(violations, path+" contains business condition in construction")
					}
				}
				if call, ok := node.(*ast.CallExpr); ok {
					switch function := call.Fun.(type) {
					case *ast.SelectorExpr:
						if !strings.HasPrefix(function.Sel.Name, "New") && function.Sel.Name != "Compose" && function.Sel.Name != "Errorf" {
							violations = append(violations, path+" calls business function in construction "+function.Sel.Name)
						}
					}
				}
				switch node.(type) {
				case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.GoStmt, *ast.DeferStmt:
					violations = append(violations, path+" contains business control flow in construction")
				}
				return true
			})
		}
	}
	if layer != "contracts" {
		return violations
	}
	checkResults := func(function *ast.FuncType) {
		if function.Results != nil {
			for _, field := range function.Results.List {
				if forbiddenContractResult(field.Type, imports) {
					violations = append(violations, path+" public contract result escapes observation or iterator")
				}
			}
		}
	}
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.IsExported() {
			checkResults(function.Type)
		}
		group, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			typ, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			switch value := typ.Type.(type) {
			case *ast.StructType:
				for _, field := range value.Fields.List {
					if forbiddenContractResult(field.Type, imports) {
						violations = append(violations, path+" public contract field escapes observation or iterator")
					}
				}
			case *ast.InterfaceType:
				for _, field := range value.Methods.List {
					for _, name := range field.Names {
						switch name.Name {
						case "Query", "QueryContext", "QueryRow", "QueryRowContext", "Exec", "ExecContext", "Begin", "BeginTx", "Commit", "Rollback", "RawTx":
							violations = append(violations, path+" exposes generic storage capability "+name.Name)
						}
					}
					if method, ok := field.Type.(*ast.FuncType); ok {
						checkResults(method)
					}
				}
			default:
				if forbiddenContractResult(typ.Type, imports) {
					violations = append(violations, path+" public contract aliases private type or escaping capability")
				}
			}
		}
	}

	return violations
}

func forbiddenContractResult(expr ast.Expr, imports map[string]string) bool {
	bad := false
	ast.Inspect(expr, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.Ident:
			if value.Name == "any" {
				bad = true
			}
		case *ast.FuncType:
			bad = true
		case *ast.ChanType:
			bad = true
		case *ast.SelectorExpr:
			if pkg, ok := value.X.(*ast.Ident); ok {
				path := imports[pkg.Name]
				if path == "database/sql" || strings.HasPrefix(path, "net/") || path == "iter" || strings.Contains(path, "/internal/") && path != internalPrefix+"exact" && path != internalPrefix+"storage/contracts" && !strings.HasSuffix(path, "/contracts") || value.Sel.Name == "ObservationRef" || strings.Contains(path, "/adapters/") {
					bad = true
				}
			}
		}
		return true
	})
	return bad
}

func importCycles(graph map[string]map[string]bool) []string {
	state := map[string]int{}
	var stack, violations []string
	var visit func(string)
	visit = func(node string) {
		if state[node] == 2 {
			return
		}
		if state[node] == 1 {
			violations = append(violations, fmt.Sprintf("import cycle %s -> %s", strings.Join(stack, " -> "), node))
			return
		}
		state[node] = 1
		stack = append(stack, node)
		targets := make([]string, 0, len(graph[node]))
		for target := range graph[node] {
			if graph[target] != nil {
				targets = append(targets, target)
			}
		}
		sort.Strings(targets)
		for _, target := range targets {
			visit(target)
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
	}
	nodes := make([]string, 0, len(graph))
	for node := range graph {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		visit(node)
	}
	return violations
}

func isNil(expr ast.Expr) bool { value, ok := expr.(*ast.Ident); return ok && value.Name == "nil" }
func isErrorIdentifier(expr ast.Expr) bool {
	value, ok := expr.(*ast.Ident)
	return ok && (value.Name == "err" || strings.HasSuffix(value.Name, "Err"))
}

func moduleCycles(packages map[string]map[string]bool) []string {
	graph := map[string]map[string]bool{}
	for source, targets := range packages {
		owner, _ := modulePath(source)
		if owner == "" {
			continue
		}
		node := "module:" + owner
		if graph[node] == nil {
			graph[node] = map[string]bool{}
		}
		for target := range targets {
			other, _ := modulePath(target)
			if other != "" && other != owner {
				graph[node]["module:"+other] = true
				if graph["module:"+other] == nil {
					graph["module:"+other] = map[string]bool{}
				}
			}
		}
	}
	return importCycles(graph)
}
