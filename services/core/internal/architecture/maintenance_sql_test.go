package architecture

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"omni-folio/services/core/internal/storage/ownership"
)

const maintenanceSQLite = "modules/maintenance/internal/adapters/sqlite/"

func containingFunction(file *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Body != nil && function.Pos() <= pos && pos < function.End() {
			return function
		}
	}
	return nil
}

func sqlArgument(call *ast.CallExpr) (int, string) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return -1, ""
	}
	switch selector.Sel.Name {
	case "Query", "QueryRow", "Exec", "Prepare":
		return 0, selector.Sel.Name
	case "QueryContext", "QueryRowContext", "ExecContext", "PrepareContext", "QueryOne", "Execute":
		return 1, selector.Sel.Name
	}
	return -1, ""
}

func sourceImport(file *ast.File, alias string) string {
	for _, item := range file.Imports {
		path, err := strconv.Unquote(item.Path.Value)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		if item.Name != nil {
			name = item.Name.Name
		}
		if name == alias {
			return path
		}
	}
	return ""
}

func approvedMaintenanceSQL(source parsedSource, call *ast.CallExpr, query string, known bool, sources []parsedSource) bool {
	if !strings.HasPrefix(source.path, maintenanceSQLite) {
		return false
	}
	function := containingFunction(source.file, call.Pos())
	if function == nil {
		return false
	}
	index, method := sqlArgument(call)
	if index < 0 || index >= len(call.Args) {
		return false
	}
	file := strings.TrimPrefix(source.path, maintenanceSQLite)
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	if known {
		if file == "migrate.go" && function.Name.Name == "Migrate" && method == "QueryRowContext" && len(call.Args) == 2 && normalized == "select count(*) from sqlite_master where type='table' and name='schema_migrations'" {
			return true
		}
		if file == "candidate.go" && function.Name.Name == "verifyCandidate" && method == "QueryRowContext" && normalized == "pragma integrity_check" {
			return len(call.Args) == 2
		}
		if file == "migrate.go" && function.Name.Name == "migrateVersion" {
			if method == "Exec" && (normalized == "pragma foreign_keys=on" || normalized == "pragma foreign_keys=off") {
				return len(call.Args) == 1
			}
			if method == "QueryRow" && (normalized == "pragma foreign_keys" || normalized == "select count(*) from pragma_foreign_key_check") {
				return len(call.Args) == 1
			}
		}
		if file == "restore_schema.go" && function.Name.Name == "requireOrderRestoreSchema" && method == "Query" && normalized == "pragma foreign_key_check" {
			return len(call.Args) == 1
		}
		metadata := file == "restore_schema.go" && (function.Name.Name == "requireOrderRestoreSchema" || function.Name.Name == "hasUniqueIndex") || file == "migrate.go" && (function.Name.Name == "migration20DependentTriggerInventory" || function.Name.Name == "migration20ForeignKeyInventory" || function.Name.Name == "verifyMigration20TemporaryNames" || function.Name.Name == "migration27SchemaInventory")
		if metadata && (method == "Query" || method == "QueryRow" || method == "QueryContext" || method == "QueryRowContext") && ownership.ValidateMaintenanceCatalogRead(query) == nil {
			receiver := call.Fun.(*ast.SelectorExpr).X
			for _, argument := range call.Args[index+1:] {
				if !metadataNameOrigin(source, function, argument, receiver, sources, map[*ast.Object]bool{}) {
					return false
				}
			}
			return true
		}
		if file == "verify_manifest.go" && function.Name.Name == "VerifyManifest" && method == "QueryRow" && len(call.Args) == 1 {
			for _, statement := range []string{"SELECT COUNT(*) FROM sqlite_master WHERE name='owner_fx_series_versions'", "SELECT COUNT(*) FROM sqlite_master WHERE name='owner_price_series_versions'", "SELECT COUNT(*) FROM sqlite_master WHERE name='watchlist_state'", "SELECT COUNT(*) FROM sqlite_master WHERE name IN ('paper_promotion_claims','paper_promotion_decisions')"} {
				if normalized == strings.ToLower(statement) {
					return true
				}
			}
		}
	}
	if file == "backup.go" && function.Name.Name == "CreateBackup" && method == "Exec" && len(call.Args) == 1 {
		return vacuumDestinationEscaped(source, function, call.Args[0])
	}
	return false
}

func metadataNameOrigin(source parsedSource, function *ast.FuncDecl, expr, receiver ast.Expr, sources []parsedSource, seen map[*ast.Object]bool) bool {
	if text, known := constantString(expr, source.constants, map[string]bool{}); known {
		_, owned := ownership.ObjectOwner(text)
		return owned || text == "pk" || text == "u"
	}
	field := ""
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		field = selector.Sel.Name
		expr = selector.X
	}
	name, ok := expr.(*ast.Ident)
	if !ok || name.Obj == nil || seen[name.Obj] {
		return false
	}
	seen[name.Obj] = true
	defer delete(seen, name.Obj)
	var binding any = name.Obj.Decl
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if statement, ok := node.(*ast.RangeStmt); ok {
			for _, expr := range []ast.Expr{statement.Key, statement.Value} {
				if identifier, ok := expr.(*ast.Ident); ok && identifier.Obj == name.Obj {
					binding = statement
				}
			}
		}
		return true
	})
	switch declaration := binding.(type) {
	case *ast.RangeStmt:
		if objectAssignments(function, name.Obj) != 0 {
			return false
		}
		values := declaration.X
		if identifier, ok := values.(*ast.Ident); ok && identifier.Obj != nil {
			if assignment, ok := identifier.Obj.Decl.(*ast.AssignStmt); ok && len(assignment.Rhs) == 1 {
				if objectReferences(function, identifier.Obj) != 2 || objectAssignments(function, identifier.Obj) != 1 {
					return false
				}
				values = assignment.Rhs[0]
			}
		}
		if literal, ok := values.(*ast.CompositeLit); ok {
			key, _ := declaration.Key.(*ast.Ident)
			return literalMetadataNames(source, literal, field, key != nil && key.Obj == name.Obj)
		}
		if field != "" {
			return false
		}
		identifier, ok := values.(*ast.Ident)
		if !ok || identifier.Obj == nil {
			return false
		}
		var appended ast.Expr
		assignments := 0
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, left := range assignment.Lhs {
				if target, ok := left.(*ast.Ident); ok && target.Obj == identifier.Obj {
					assignments++
					if len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 {
						if call, ok := assignment.Rhs[0].(*ast.CallExpr); ok && astShape(call.Fun) == "append" && len(call.Args) == 2 && astShape(call.Args[0]) == identifier.Name {
							appended = call.Args[1]
						}
					}
				}
			}
			return true
		})
		return assignments == 1 && objectReferences(function, identifier.Obj) == 4 && appended != nil && metadataNameOrigin(source, function, appended, receiver, sources, seen)
	case *ast.Field:
		if objectAssignments(function, name.Obj) != 0 {
			return false
		}
		if function.Name.Name != "hasUniqueIndex" || field != "" || (name.Name != "table" && name.Name != "origin") {
			return false
		}
		count, references := 0, 0
		valid := true
		for _, callerSource := range sources {
			if filepath.Dir(callerSource.path) != filepath.Dir(source.path) {
				continue
			}
			references += identifierCount(callerSource.file, "hasUniqueIndex")
			ast.Inspect(callerSource.file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || astShape(call.Fun) != "hasUniqueIndex" {
					return true
				}
				count++
				caller := containingFunction(callerSource.file, call.Pos())
				if callerSource.path != source.path || caller == nil || caller.Name.Name != "requireOrderRestoreSchema" || len(call.Args) != 4 {
					valid = false
					return true
				}
				argument := 1
				if name.Name == "origin" {
					argument = 3
				}
				if !metadataNameOrigin(callerSource, caller, call.Args[argument], call.Args[0], sources, seen) {
					valid = false
				}
				return true
			})
		}
		return valid && count > 0 && references == count+1
	case *ast.ValueSpec:
		if len(declaration.Values) != 0 {
			return false
		}
		writes := false
		scanned := false
		var scanAddresses []*ast.UnaryExpr
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if assignment, ok := node.(*ast.AssignStmt); ok {
				for _, left := range assignment.Lhs {
					ast.Inspect(left, func(node ast.Node) bool {
						if identifier, ok := node.(*ast.Ident); ok && identifier.Obj == name.Obj {
							writes = true
						}
						return true
					})
				}
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Scan" {
				return true
			}
			for _, argument := range call.Args {
				address, ok := argument.(*ast.UnaryExpr)
				if !ok || address.Op != token.AND {
					continue
				}
				wanted := name.Name
				if field != "" {
					wanted += "." + field
				}
				if astShape(address.X) != wanted {
					continue
				}
				rows, ok := selector.X.(*ast.Ident)
				if !ok || rows.Obj == nil {
					continue
				}
				assignment, ok := rows.Obj.Decl.(*ast.AssignStmt)
				if !ok || len(assignment.Rhs) != 1 || objectAssignments(function, rows.Obj) != 1 {
					continue
				}
				query, ok := assignment.Rhs[0].(*ast.CallExpr)
				if !ok {
					continue
				}
				sqlIndex, method := sqlArgument(query)
				if sqlIndex < 0 || sqlIndex >= len(query.Args) || !strings.HasPrefix(method, "Query") {
					continue
				}
				producer := query.Fun.(*ast.SelectorExpr).X
				text, known := constantString(query.Args[sqlIndex], source.constants, map[string]bool{})
				origin, originOK := producer.(*ast.Ident)
				target, targetOK := receiver.(*ast.Ident)
				if known && originOK && targetOK && origin.Obj != nil && origin.Obj == target.Obj && objectAssignments(function, target.Obj) == 0 && !addressEscapes(function, target.Obj, nil) && ownership.ValidateMaintenanceCatalogRead(text) == nil {
					scanned = true
					scanAddresses = append(scanAddresses, address)
				}
			}
			return true
		})
		return scanned && !writes && !addressEscapes(function, name.Obj, scanAddresses)
	}
	return false
}

func literalMetadataNames(source parsedSource, literal *ast.CompositeLit, field string, key bool) bool {
	var structure *ast.StructType
	switch typ := literal.Type.(type) {
	case *ast.ArrayType:
		structure, _ = typ.Elt.(*ast.StructType)
	case *ast.MapType:
		structure, _ = typ.Value.(*ast.StructType)
	}
	fieldIndex := -1
	if field != "" {
		if structure == nil {
			return false
		}
		index := 0
		for _, item := range structure.Fields.List {
			for _, name := range item.Names {
				if name.Name == field {
					fieldIndex = index
				}
				index++
			}
		}
		if fieldIndex < 0 {
			return false
		}
	}
	for _, element := range literal.Elts {
		value := element
		if pair, ok := element.(*ast.KeyValueExpr); ok {
			if key {
				value = pair.Key
			} else {
				value = pair.Value
			}
		}
		if field != "" {
			record, ok := value.(*ast.CompositeLit)
			if !ok || fieldIndex >= len(record.Elts) {
				return false
			}
			value = record.Elts[fieldIndex]
			if pair, ok := value.(*ast.KeyValueExpr); ok {
				identifier, ok := pair.Key.(*ast.Ident)
				if !ok || identifier.Name != field {
					return false
				}
				value = pair.Value
			}
		}
		text, known := constantString(value, source.constants, map[string]bool{})
		if !known {
			return false
		}
		if _, owned := ownership.ObjectOwner(text); !owned && text != "pk" && text != "u" {
			return false
		}
	}
	return len(literal.Elts) > 0
}

func vacuumDestinationEscaped(source parsedSource, function *ast.FuncDecl, expr ast.Expr) bool {
	outer, ok := expr.(*ast.BinaryExpr)
	if !ok || outer.Op != token.ADD {
		return false
	}
	tail, known := constantString(outer.Y, source.constants, map[string]bool{})
	if !known || tail != "'" {
		return false
	}
	inner, ok := outer.X.(*ast.BinaryExpr)
	if !ok || inner.Op != token.ADD {
		return false
	}
	prefix, known := constantString(inner.X, source.constants, map[string]bool{})
	if !known || prefix != "VACUUM INTO '" {
		return false
	}
	quoted, ok := inner.Y.(*ast.Ident)
	if !ok || quoted.Obj == nil {
		return false
	}
	assignment, ok := quoted.Obj.Decl.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 3 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "ReplaceAll" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || sourceImport(source.file, pkg.Name) != "strings" {
		return false
	}
	from, a := constantString(call.Args[1], source.constants, map[string]bool{})
	to, b := constantString(call.Args[2], source.constants, map[string]bool{})
	references := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok && identifier.Obj == quoted.Obj {
			references++
		}
		return true
	})
	return a && b && from == "'" && to == "''" && references == 2
}

func TestMaintenanceMetadataObjectProvenance(t *testing.T) {
	cases := []struct {
		name, source string
		allow        bool
	}{
		{"fixed table list", `package sqlite;func requireOrderRestoreSchema(db DB){for _,table:=range []string{"events","order_events"}{db.QueryRow("SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?",table)}}`, true},
		{"external object", `package sqlite;func requireOrderRestoreSchema(db DB,table string){db.QueryRow("SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?",table)}`, false},
		{"foreign business count", `package sqlite;func requireOrderRestoreSchema(db DB){db.QueryRow("SELECT count(*) FROM events")}`, false},
		{"foreign join", `package sqlite;func requireOrderRestoreSchema(db DB){db.QueryRow("SELECT * FROM sqlite_master JOIN events ON 1=1")}`, false},
		{"inventoried index", `package sqlite;func hasUniqueIndex(db DB){rows,_:=db.Query("SELECT name FROM pragma_index_list('events')");var indexes []string;for rows.Next(){var name string;rows.Scan(&name);indexes=append(indexes,name)};for _,index:=range indexes{db.Query("SELECT name FROM pragma_index_info(?)",index)}}`, true},
		{"other DB index", `package sqlite;func hasUniqueIndex(db DB,other DB){rows,_:=other.Query("SELECT name FROM pragma_index_list('events')");var indexes []string;for rows.Next(){var name string;rows.Scan(&name);indexes=append(indexes,name)};for _,index:=range indexes{db.Query("SELECT name FROM pragma_index_info(?)",index)}}`, false},
		{"escaped index name", `package sqlite;func hasUniqueIndex(db DB){rows,_:=db.Query("SELECT name FROM pragma_index_list('events')");var indexes []string;for rows.Next(){var name string;rows.Scan(&name);rewrite(&name);indexes=append(indexes,name)};for _,index:=range indexes{db.Query("SELECT name FROM pragma_index_info(?)",index)}}`, false},
		{"mutated fixed list", `package sqlite;func requireOrderRestoreSchema(db DB,external string){tables:=[]string{"events"};tables[0]=external;for _,table:=range tables{db.QueryRow("SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?",table)}}`, false},
		{"aliased fixed list", `package sqlite;func requireOrderRestoreSchema(db DB,external string){tables:=[]string{"events"};alias:=tables;alias[0]=external;for _,table:=range tables{db.QueryRow("SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?",table)}}`, false},
		{"escaped fixed list", `package sqlite;func requireOrderRestoreSchema(db DB){tables:=[]string{"events"};rewrite(&tables);for _,table:=range tables{db.QueryRow("SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?",table)}}`, false},
		{"escaped captured list", `package sqlite;func hasUniqueIndex(db DB){rows,_:=db.Query("SELECT name FROM pragma_index_list('events')");var indexes []string;for rows.Next(){var name string;rows.Scan(&name);indexes=append(indexes,name)};rewrite(indexes);for _,index:=range indexes{db.Query("SELECT name FROM pragma_index_info(?)",index)}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := inspectSQLSources(fixtureSources(t, map[string]string{maintenanceSQLite + "restore_schema.go": tc.source}))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != tc.allow {
				t.Fatalf("allow %t, got %v", tc.allow, violations)
			}
		})
	}
}

func TestMetadataParameterCallersStayClosed(t *testing.T) {
	for _, call := range []string{"", `hasUniqueIndex(db,external,[]string{"id"},"u")`, `f:=hasUniqueIndex;f(db,external,[]string{"id"},"u")`} {
		sources := map[string]string{maintenanceSQLite + "restore_schema.go": `package sqlite;func requireOrderRestoreSchema(db DB){hasUniqueIndex(db,"events",[]string{"id"},"u")};func hasUniqueIndex(db DB,table string,columns []string,origin string){db.Query("SELECT name FROM pragma_index_list(?) WHERE origin=?",table,origin)}`}
		if call != "" {
			sources[maintenanceSQLite+"unsafe.go"] = `package sqlite;func unsafe(db DB,external string){` + call + `}`
		}
		violations, err := inspectSQLSources(fixtureSources(t, sources))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != (call == "") {
			t.Fatalf("caller %s, got %v", call, violations)
		}
	}
}

func TestMetadataReceiverStaysBoundToInventory(t *testing.T) {
	for _, change := range []string{"", "replaced", "escaped", "shadowed"} {
		source := `package sqlite;func hasUniqueIndex(db DB,other DB){rows,_:=db.Query("SELECT name FROM pragma_index_list('events')");var indexes []string;for rows.Next(){var name string;rows.Scan(&name);indexes=append(indexes,name)};for _,index:=range indexes{db.Query("SELECT name FROM pragma_index_info(?)",index)}}`
		switch change {
		case "replaced":
			source = strings.Replace(source, ";for _,index", ";db=other;for _,index", 1)
		case "escaped":
			source = strings.Replace(source, ";for _,index", ";rewrite(&db);for _,index", 1)
		case "shadowed":
			source = strings.Replace(source, ";for _,index", ";{db:=other;for _,index", 1) + "}"
		}
		violations, err := inspectSQLSources(fixtureSources(t, map[string]string{maintenanceSQLite + "restore_schema.go": source}))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != (change == "") {
			t.Fatalf("change %s, got %v", change, violations)
		}
	}
}

func TestMaintenanceVacuumDestinationEscaping(t *testing.T) {
	for _, escaped := range []bool{false, true} {
		replacement := "'"
		if escaped {
			replacement = "''"
		}
		source := `package sqlite;import "strings";func(s *Store)CreateBackup(out string){quoted:=strings.ReplaceAll(out,"'","` + replacement + `");db.Exec("VACUUM INTO '"+quoted+"'")}`
		violations, err := inspectSQLSources(fixtureSources(t, map[string]string{maintenanceSQLite + "backup.go": source}))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != escaped {
			t.Fatalf("escaped %t, got %v", escaped, violations)
		}
	}
}

func objectAssignments(function *ast.FuncDecl, object *ast.Object) int {
	count := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, left := range assignment.Lhs {
			found := false
			ast.Inspect(left, func(node ast.Node) bool {
				if name, ok := node.(*ast.Ident); ok && name.Obj == object {
					found = true
				}
				return true
			})
			if found {
				count++
			}
		}
		return true
	})
	return count
}

func objectReferences(node ast.Node, object *ast.Object) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		if name, ok := node.(*ast.Ident); ok && name.Obj == object {
			count++
		}
		return true
	})
	return count
}

func addressEscapes(function *ast.FuncDecl, object *ast.Object, allowed []*ast.UnaryExpr) bool {
	approved := map[*ast.UnaryExpr]bool{}
	for _, address := range allowed {
		approved[address] = true
	}
	escaped := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		address, ok := node.(*ast.UnaryExpr)
		if !ok || address.Op != token.AND || approved[address] {
			return true
		}
		ast.Inspect(address.X, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && name.Obj == object {
				escaped = true
			}
			return true
		})
		return true
	})
	return escaped
}
