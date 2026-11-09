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
	"research":    {},
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
		if owner == "research" && (name == "database/sql" || strings.HasPrefix(name, "database/sql/") || strings.HasPrefix(name, internalPrefix+"storage/") || name == "omni-folio/services/core/migrations") {
			return "research compatibility module has no storage authority " + name
		}
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
		if (strings.HasPrefix(relative, "providers/") || strings.HasPrefix(relative, "transport/")) && target != "" {
			return "provider SDK imports business module " + name
		}
		if strings.HasPrefix(relative, "codec/") && !pureModuleImport(name) && name != "io" {
			return "shared codec imports impure dependency " + name
		}
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
		if name == internalPrefix+"storage/contracts" {
			return ""
		}
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
		if !rootConstructionFile(relative) {
			return "module root must use a fixed construction entrypoint file"
		}
	case "adapters":
		if name == internalPrefix+"codec" || name == internalPrefix+"providers/kiwoom" || name == internalPrefix+"transport/httpjson" {
			return ""
		}
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
		if !rootConstructionFile(path) {
			violations = append(violations, path+" module root is construction only")
		}
		for _, declaration := range file.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.VAR {
				violations = append(violations, path+" module construction cannot expose package state")
			}
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.TYPE {
				for _, spec := range group.Specs {
					typ := spec.(*ast.TypeSpec)
					if !typ.Name.IsExported() {
						continue
					}
					if typ.Name.Name == "Config" || typ.Name.Name == "Dependencies" {
						structure, ok := typ.Type.(*ast.StructType)
						if !ok {
							violations = append(violations, path+" construction config must be a struct")
							continue
						}
						for _, field := range structure.Fields.List {
							if forbiddenConstructionInput(field.Type, imports, owner) {
								violations = append(violations, path+" construction config exposes private or scoped capability")
							}
						}
					} else if forbiddenContractResult(typ.Type, imports) {
						violations = append(violations, path+" construction result exposes private capability")
					}
				}
			}
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if !rootConstructionEntry(path, owner, function.Name.Name) || function.Recv != nil {
				violations = append(violations, path+" module root permits fixed construction entrypoints only")
				continue
			}
			if function.Type.Results != nil {
				for _, field := range function.Type.Results.List {
					if forbiddenConstructionResult(field.Type, imports, function.Name.Name) {
						violations = append(violations, path+" construction result exposes private capability")
					}
				}
			}
			entryName := function.Name.Name
			if function.Type.Params != nil {
				for _, field := range function.Type.Params.List {
					if forbiddenConstructionInput(field.Type, imports, owner) {
						violations = append(violations, path+" construction input exposes private capability")
					}
				}
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if condition, ok := node.(*ast.IfStmt); ok && !constructionCondition(condition.Cond) {
					violations = append(violations, path+" contains business condition in construction")
				}

				if call, ok := node.(*ast.CallExpr); ok {
					switch function := call.Fun.(type) {
					case *ast.SelectorExpr:
						pkgPath := ""
						if pkg, ok := function.X.(*ast.Ident); ok {
							pkgPath = imports[pkg.Name]
						}
						if !constructionCall(function.Sel.Name) && !transportConstructionCall(entryName, pkgPath, owner, function.Sel.Name) {
							violations = append(violations, path+" calls business function in construction "+function.Sel.Name)
						}
					case *ast.Ident:
						if function.Name != "new" && function.Name != "make" {
							violations = append(violations, path+" calls arbitrary callback in construction "+function.Name)
						}
					default:
						violations = append(violations, path+" calls arbitrary callback in construction")
					}
				}
				if _, ok := node.(*ast.FuncLit); ok {
					violations = append(violations, path+" defines callback implementation in construction")
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
		if function.Params != nil {
			for _, field := range function.Params.List {
				if forbiddenContractInput(field.Type, imports) {
					violations = append(violations, path+" public contract accepts unbounded callback or resource")
				}
			}
		}
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
		if group.Tok == token.VAR {
			for _, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				for _, initializer := range value.Values {
					call, ok := initializer.(*ast.CallExpr)
					approved := false
					if ok {
						if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
							if pkg, ok := selector.X.(*ast.Ident); ok {
								approved = imports[pkg.Name] == "errors" && selector.Sel.Name == "New"
							}
						}
					}
					if !approved {
						violations = append(violations, path+" public contract contains mutable capability state")
					}
				}
				if len(value.Values) == 0 {
					violations = append(violations, path+" public contract contains mutable capability state")
				}
			}
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
				if value.Methods.NumFields() == 0 {
					violations = append(violations, path+" public contract exposes unbounded interface")
				}
				for _, field := range value.Methods.List {
					for _, name := range field.Names {
						switch name.Name {
						case "Query", "QueryContext", "QueryRow", "QueryRowContext", "Exec", "ExecContext", "Begin", "BeginTx", "Commit", "Rollback", "RawTx", "Next", "Scan", "Columns", "ColumnTypes":
							violations = append(violations, path+" exposes generic storage capability "+name.Name)
						}
					}
					if method, ok := field.Type.(*ast.FuncType); ok {
						checkResults(method)
					} else if forbiddenContractResult(field.Type, imports) {
						violations = append(violations, path+" public contract embeds escaping capability")
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
		case *ast.InterfaceType:
			if value.Methods.NumFields() == 0 {
				bad = true
			}
		case *ast.ChanType:
			bad = true
		case *ast.SelectorExpr:
			if pkg, ok := value.X.(*ast.Ident); ok {
				path := imports[pkg.Name]

				if path == "database/sql" || path == "context" && value.Sel.Name == "Context" || path == "time" && (value.Sel.Name == "Timer" || value.Sel.Name == "Ticker") || strings.HasPrefix(path, "net/") || path == "iter" || strings.Contains(path, "/internal/") && path != internalPrefix+"exact" && path != internalPrefix+"storage/contracts" && !strings.HasSuffix(path, "/contracts") || value.Sel.Name == "ObservationRef" || strings.Contains(path, "/adapters/") {
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

func constructionCall(name string) bool {
	if strings.HasPrefix(name, "New") || name == "Compose" || name == "Errorf" {
		return true
	}
	switch name {
	case "MatchesDB", "LedgerReader", "MarketReader", "WatchlistReader", "OwnerPriceWriter", "ReadStatements", "OwnerPriceStatements", "MigrationStarter":
		return true
	}
	return false
}
func constructionCondition(expr ast.Expr) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return constructionCondition(value.X)
	case *ast.BinaryExpr:
		if value.Op == token.LOR || value.Op == token.LAND {
			return constructionCondition(value.X) && constructionCondition(value.Y)
		}
		if value.Op != token.EQL && value.Op != token.NEQ {
			return false
		}
		if !isNil(value.Y) {
			return false
		}
		if isErrorIdentifier(value.X) {
			return true
		}
		_, ok := value.X.(*ast.SelectorExpr)
		return ok
	case *ast.UnaryExpr:
		if value.Op == token.NOT {
			return constructionCondition(value.X)
		}
	case *ast.CallExpr:
		selector, ok := value.Fun.(*ast.SelectorExpr)
		return ok && selector.Sel.Name == "MatchesDB"
	}
	return false
}

func forbiddenConstructionInput(expr ast.Expr, imports map[string]string, owner string) bool {
	bad := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if pkg, ok := selector.X.(*ast.Ident); ok {
				path := imports[pkg.Name]
				if path == internalPrefix+"storage/observation" && selector.Sel.Name == "MigrationStarter" && owner != "maintenance" {
					bad = true
				}
				if path == "database/sql" && owner == "research" {
					bad = true
				}
				if path == "database/sql" && selector.Sel.Name != "DB" {
					bad = true
				}
				if path == internalPrefix+"providers/kiwoom" && selector.Sel.Name != "Client" {
					bad = true
				}
				if path == internalPrefix+"storage/observation" && selector.Sel.Name != "Registry" && selector.Sel.Name != "ReadResolver" && selector.Sel.Name != "ImportResolver" && selector.Sel.Name != "MigrationStarter" {
					bad = true
				}
				if strings.Contains(path, "/modules/") && !strings.HasSuffix(path, "/contracts") {
					bad = true
				}
			}
		}
		return true
	})
	return bad
}

func forbiddenContractInput(expr ast.Expr, imports map[string]string) bool {
	bad := false
	ast.Inspect(expr, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncType, *ast.ChanType:
			bad = true
		case *ast.Ident:
			if value.Name == "any" {
				bad = true
			}
		case *ast.InterfaceType:
			if value.Methods.NumFields() == 0 {
				bad = true
			}
		case *ast.SelectorExpr:
			if pkg, ok := value.X.(*ast.Ident); ok && imports[pkg.Name] == "time" && (value.Sel.Name == "Timer" || value.Sel.Name == "Ticker") {
				bad = true
			}
		}
		return true
	})
	return bad
}

func rootConstructionFile(path string) bool {
	switch filepath.Base(path) {
	case "compose.go", "register_http.go", "compose_http.go", "run_cli.go", "cli.go":
		return true
	}
	return false
}
func rootConstructionEntry(path, owner, name string) bool {
	switch filepath.Base(path) {
	case "compose.go":
		return name == "Compose" || owner == "marketdata" && name == "ComposeOwnerPrice" || name == "ComposeHTTP"
	case "register_http.go":
		return name == "RegisterHTTP"
	case "compose_http.go":
		return name == "ComposeHTTP"
	case "run_cli.go", "cli.go":
		return name == "RunCLI"
	}
	return false
}
func transportConstructionCall(entry, pkgPath, owner, name string) bool {
	prefix := internalPrefix + "modules/" + owner + "/internal/adapters/"
	if entry == "RegisterHTTP" && (pkgPath == prefix+"http" || strings.HasPrefix(pkgPath, prefix+"http/")) {
		return name == "Register" || name == "RegisterHTTP"
	}
	if entry == "RunCLI" && (pkgPath == prefix+"cli" || strings.HasPrefix(pkgPath, prefix+"cli/")) {
		return name == "Run" || name == "RunCLI"
	}
	return false
}
func forbiddenConstructionResult(expr ast.Expr, imports map[string]string, entry string) bool {
	if entry != "ComposeHTTP" {
		return forbiddenContractResult(expr, imports)
	}
	sanitized := map[string]string{}
	for alias, path := range imports {
		sanitized[alias] = path
		if path == "net/http" {
			sanitized[alias] = "construction/http"
		}
	}
	bad := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			if pkg, ok := selector.X.(*ast.Ident); ok && imports[pkg.Name] == "net/http" && selector.Sel.Name != "Handler" {
				bad = true
			}
		}
		return true
	})
	return bad || forbiddenContractResult(expr, sanitized)
}
