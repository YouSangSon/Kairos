package architecture

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"omni-folio/services/core/internal/storage/ownership"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func migrationSources(root string) ([]parsedSource, error) {
	directory := filepath.Join(root, "..", "migrations")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []parsedSource
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, filepath.Join(directory, entry.Name()), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		result = append(result, parsedSource{path: "../migrations/" + entry.Name(), file: file, positions: positions, constants: map[string]ast.Expr{}})
	}
	return result, nil
}

func findSource(sources []parsedSource, path string) *parsedSource {
	for i := range sources {
		if sources[i].path == path {
			return &sources[i]
		}
	}
	return nil
}
func namedFunction(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}
func identifierCount(node ast.Node, name string) int {
	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok && identifier.Name == name {
			count++
		}
		return true
	})
	return count
}
func oneCall(function *ast.FuncDecl, target string) *ast.CallExpr {
	var found *ast.CallExpr
	count := 0
	if function == nil {
		return nil
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && astShape(call.Fun) == target {
			count++
			found = call
		}
		return true
	})
	if count != 1 {
		return nil
	}
	return found
}
func declarationValue(file *ast.File, name string) ast.Expr {
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, identifier := range value.Names {
				if identifier.Name == name && index < len(value.Values) {
					return value.Values[index]
				}
			}
		}
	}
	return nil
}

func embeddedMigrationProgram(sources []parsedSource) bool {
	source := findSource(sources, maintenanceSQLite+"migrate.go")
	if source == nil || sourceImport(source.file, "migrations") != "omni-folio/services/core/migrations" {
		return false
	}
	names, ok := declarationValue(source.file, "migrationNames").(*ast.CompositeLit)
	if !ok || len(names.Elts) != 27 {
		return false
	}
	var history map[string]string
	if json.Unmarshal(migrationHistory, &history) != nil {
		return false
	}
	var expected []string
	for name := range history {
		expected = append(expected, name)
	}
	sort.Strings(expected)
	if len(expected) != 27 {
		return false
	}
	for index, item := range names.Elts {
		actual, known := constantString(item, source.constants, map[string]bool{})
		if !known || actual != expected[index] {
			return false
		}
	}
	namesUsed, versionUsed := 0, 0
	for _, file := range sources {
		if filepath.Dir(file.path) == filepath.Dir(source.path) {
			namesUsed += identifierCount(file.file, "migrationNames")
			versionUsed += identifierCount(file.file, "migrateVersion")
		}
	}
	if namesUsed != 2 || versionUsed != 2 {
		return false
	}
	migrate := namedFunction(source.file, "Migrate")
	version := namedFunction(source.file, "migrateVersion")
	if migrate == nil || version == nil || identifierCount(migrate, "script") != 2 || identifierCount(version, "script") != 2 {
		return false
	}
	read := oneCall(migrate, "migrations.Files.ReadFile")
	execute := oneCall(migrate, "s.migrateVersion")
	if read != nil {
		outer, ok := read.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		inner, ok := outer.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := inner.X.(*ast.Ident)
		if !ok || pkg.Obj != nil {
			return false
		}
	}
	if read == nil || execute == nil || astShape(read) != `migrations.Files.ReadFile("migrations/" + migrationNames[version-1])` || astShape(execute) != "s.migrateVersion(ctx, version, script)" {
		return false
	}
	scriptBound := false
	ast.Inspect(migrate.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 && assignment.Rhs[0] == read && astShape(assignment.Lhs[0]) == "script" {
			left, ok := assignment.Lhs[0].(*ast.Ident)
			right, other := execute.Args[2].(*ast.Ident)
			scriptBound = ok && other && left.Obj == right.Obj
		}
		return true
	})
	if !scriptBound {
		return false
	}
	loopBound := false
	ast.Inspect(migrate.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.ForStmt)
		if !ok {
			return true
		}
		if astShape(loop.Init) == "version := current + 1" && astShape(loop.Cond) == "version <= latestSchema" && astShape(loop.Post) == "version++" && loop.Body.Pos() < read.Pos() && execute.End() < loop.Body.End() && read.Pos() < execute.Pos() {
			loopBound = true
		}
		return true
	})
	return loopBound && embeddedDriver(sources) && migrationTransaction(version)
}

func embeddedDriver(sources []parsedSource) bool {
	driver := findSource(sources, "../migrations/embed.go")
	if driver == nil || sourceImport(driver.file, "embed") != "embed" || sourceImport(driver.file, "strings") != "strings" {
		return false
	}
	read := namedFunction(driver.file, "ReadFile")
	if read == nil || read.Body == nil || astShape(read.Type) != `func(name string) ([]byte, error)` || astShape(read.Body) != `{ return scripts.ReadFile(strings.TrimPrefix(name, "migrations/")) }` {
		return false
	}
	bound, emptySource, filesSource := false, false, false
	for _, decl := range driver.file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			if typ, ok := spec.(*ast.TypeSpec); ok && typ.Name.Name == "source" {
				structure, ok := typ.Type.(*ast.StructType)
				emptySource = ok && structure.Fields.NumFields() == 0
			}
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == "Files" && astShape(value.Type) == "source" && len(value.Values) == 0 {
				filesSource = true
			}
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "scripts" || astShape(value.Type) != "embed.FS" || len(value.Values) != 0 {
				continue
			}
			if group.Doc != nil {
				for _, comment := range group.Doc.List {
					if comment.Text == "//go:embed *.sql" {
						bound = true
					}
				}
			}
		}
	}
	scripts, files := 0, 0
	for _, source := range sources {
		if strings.HasPrefix(source.path, "../migrations/") {
			scripts += identifierCount(source.file, "scripts")
			files += identifierCount(source.file, "Files")
		}
	}
	return bound && emptySource && filesSource && read.Recv != nil && len(read.Recv.List) == 1 && astShape(read.Recv.List[0].Type) == "source" && scripts == 2 && files == 1
}

func migrationTransaction(function *ast.FuncDecl) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 || len(function.Recv.List[0].Names) != 1 || function.Recv.List[0].Names[0].Name != "s" || astShape(function.Recv.List[0].Type) != "*Store" || objectAssignments(function, function.Recv.List[0].Names[0].Obj) != 0 || addressEscapes(function, function.Recv.List[0].Names[0].Obj, nil) {
		return false
	}
	begin := oneCall(function, "s.observations.MigrationStarter().Begin")
	if begin == nil || astShape(begin) != "s.observations.MigrationStarter().Begin(ctx)" {
		return false
	}
	valid := false
	writes := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, left := range assignment.Lhs {
			if identifier, ok := left.(*ast.Ident); ok && identifier.Name == "tx" {
				writes++
				if len(assignment.Lhs) == 3 && len(assignment.Rhs) == 1 && assignment.Rhs[0] == begin && astShape(assignment.Lhs[1]) == "proof" {
					valid = !addressEscapes(function, identifier.Obj, nil)
				}
			}
		}
		return true
	})
	return valid && writes == 1
}

func snapshotField(function *ast.FuncDecl, field, value, producer string, uses int) bool {
	if function == nil || oneCall(function, producer) == nil || identifierCount(function, value) != uses {
		return false
	}
	found, bound := false, false
	producerCall := oneCall(function, producer)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if ok && len(assignment.Rhs) == 1 && assignment.Rhs[0] == producerCall {
			for _, left := range assignment.Lhs {
				if astShape(left) == value {
					bound = true
				}
			}
		}
		return true
	})
	ast.Inspect(function.Body, func(node ast.Node) bool {
		pair, ok := node.(*ast.KeyValueExpr)
		if ok && astShape(pair.Key) == field && astShape(pair.Value) == value {
			found = true
		}
		return true
	})
	return found && bound
}

func capturedMigrationSchema(sources []parsedSource) bool {
	source := findSource(sources, maintenanceSQLite+"migrate.go")
	if source == nil || !embeddedMigrationProgram(sources) {
		return false
	}
	version := namedFunction(source.file, "migrateVersion")
	pre20 := oneCall(version, "s.captureMigration20Snapshot")
	pre27 := oneCall(version, "s.captureMigration27Snapshot")
	if pre20 == nil || pre27 == nil || astShape(pre20) != "s.captureMigration20Snapshot(ctx, tx, proof)" || astShape(pre27) != "s.captureMigration27Snapshot(ctx, tx, proof, false)" || identifierCount(version, "v20Before") != 5 || identifierCount(version, "v27Before") != 6 {
		return false
	}
	for _, binding := range []struct {
		name     string
		producer *ast.CallExpr
	}{{"v20Before", pre20}, {"v27Before", pre27}} {
		valid := false
		writes := 0
		ast.Inspect(version.Body, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, left := range assignment.Lhs {
				if astShape(left) == binding.name {
					writes++
					if len(assignment.Rhs) == 1 && assignment.Rhs[0] == binding.producer {
						valid = true
					}
				}
			}
			return true
		})
		if !valid || writes != 1 {
			return false
		}
	}
	wrapper := namedFunction(source.file, "captureMigration20Snapshot")
	if wrapper == nil || astShape(wrapper.Body) != "{ return s.captureMigration20SnapshotWithForeignKeyPreflight(ctx, q, proof, true) }" {
		return false
	}
	inner20 := namedFunction(source.file, "captureMigration20SnapshotWithForeignKeyPreflight")
	inner27 := namedFunction(source.file, "captureMigration27Snapshot")
	for _, function := range []*ast.FuncDecl{inner20, inner27, namedFunction(source.file, "migration20DependentTriggerInventory"), namedFunction(source.file, "migration27SchemaInventory")} {
		if !unchangedParameter(function, "q") {
			return false
		}
	}
	if !snapshotField(inner20, "DependentTriggers", "dependentTriggers", "migration20DependentTriggerInventory", 2) || !snapshotField(inner27, "SelectionIndexes", "indexes", "migration27SchemaInventory", 2) || !snapshotField(inner27, "DependentTriggers", "triggers", "migration27SchemaInventory", 2) {
		return false
	}
	call20 := oneCall(inner20, "migration20DependentTriggerInventory")
	call27 := oneCall(inner27, "migration27SchemaInventory")
	if astShape(call20) != "migration20DependentTriggerInventory(ctx, q)" || astShape(call27) != "migration27SchemaInventory(ctx, q)" {
		return false
	}
	if !callAssignment(inner20, call20, "dependentTriggers, err") || !callAssignment(inner27, call27, "objects, indexes, triggers, err") {
		return false
	}
	return capturedInventory(source, "migration20DependentTriggerInventory", "trigger", []string{"Name", "Table", "SQL"}) && capturedInventory(source, "migration27SchemaInventory", "object", []string{"Type", "Name", "Table", "SQL"})
}

func unchangedParameter(function *ast.FuncDecl, name string) bool {
	if function == nil || function.Type.Params == nil {
		return false
	}
	for _, field := range function.Type.Params.List {
		for _, identifier := range field.Names {
			if identifier.Name == name {
				return identifier.Obj != nil && objectAssignments(function, identifier.Obj) == 0 && !addressEscapes(function, identifier.Obj, nil)
			}
		}
	}
	return false
}

func capturedInventory(source *parsedSource, functionName, record string, fields []string) bool {
	function := namedFunction(source.file, functionName)
	query := oneCall(function, "q.QueryContext")
	if query == nil || len(query.Args) != 2 {
		return false
	}
	statement, known := constantString(query.Args[1], source.constants, map[string]bool{})
	if !known || ownership.ValidateMaintenanceCatalogRead(statement) != nil {
		return false
	}
	expected := `SELECT name,tbl_name,sql FROM sqlite_master WHERE type='trigger' AND tbl_name NOT IN ('execution_authority_events','strategy_selection_events','paper_performance_policy_events') AND (instr(sql, 'execution_authority_events') != 0 OR instr(sql, 'strategy_selection_events') != 0) ORDER BY name`
	if functionName == "migration27SchemaInventory" {
		expected = `SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND tbl_name NOT IN ('paper_promotion_claims','paper_promotion_decisions') ORDER BY type,name`
	}
	if strings.Join(strings.Fields(statement), " ") != expected {
		return false
	}
	scanned, written := false, false
	var captured *ast.Object
	var scanAddresses []*ast.UnaryExpr
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if assignment, ok := node.(*ast.AssignStmt); ok {
			for _, left := range assignment.Lhs {
				ast.Inspect(left, func(node ast.Node) bool {
					if name, ok := node.(*ast.Ident); ok && name.Name == record {
						written = true
					}
					return true
				})
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || astShape(call.Fun) != "rows.Scan" || len(call.Args) != len(fields) {
			return true
		}
		valid := true
		for index, field := range fields {
			if astShape(call.Args[index]) != "&"+record+"."+field {
				valid = false
			}
		}
		if valid {
			scanned = true
			for _, argument := range call.Args {
				address := argument.(*ast.UnaryExpr)
				scanAddresses = append(scanAddresses, address)
				captured = address.X.(*ast.SelectorExpr).X.(*ast.Ident).Obj
			}
		}
		return true
	})
	if functionName == "migration27SchemaInventory" {
		indexBound, triggerBound := false, false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			condition, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}
			if astShape(condition.Cond) == `object.Table == "strategy_selection_events"` {
				ast.Inspect(condition.Body, func(node ast.Node) bool {
					guard, ok := node.(*ast.IfStmt)
					if ok && astShape(guard.Cond) == `object.Type == "index" && object.SQL != ""` {
						ast.Inspect(guard.Body, func(node ast.Node) bool {
							call, ok := node.(*ast.CallExpr)
							if ok && astShape(call) == "append(indexes, object)" {
								indexBound = true
							}
							return true
						})
					}
					return true
				})
			}
			if astShape(condition.Cond) == `object.Type == "trigger" && strings.Contains(object.SQL, "strategy_selection_events")` {
				ast.Inspect(condition.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if ok && astShape(call) == "append(triggers, migration20Trigger{Name: object.Name, Table: object.Table, SQL: object.SQL})" {
						triggerBound = true
					}
					return true
				})
			}
			return true
		})
		if !indexBound || !triggerBound {
			return false
		}
	}
	outputs := map[string]string{"triggers": "append(triggers, trigger)"}
	lastReturn := "return triggers, nil"
	if functionName == "migration27SchemaInventory" {
		outputs = map[string]string{"objects": "append(objects, object)", "indexes": "append(indexes, object)", "triggers": "append(triggers, migration20Trigger{Name: object.Name, Table: object.Table, SQL: object.SQL})"}
		lastReturn = "return objects, indexes, triggers, nil"
	}
	for name, expression := range outputs {
		if identifierCount(function, name) != 4 {
			return false
		}
		bound := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 && astShape(assignment.Lhs[0]) == name && astShape(assignment.Rhs[0]) == expression {
				bound = true
			}
			return true
		})
		if !bound {
			return false
		}
	}
	if len(function.Body.List) == 0 || astShape(function.Body.List[len(function.Body.List)-1]) != lastReturn {
		return false
	}
	rowsBound := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 && assignment.Rhs[0] == query {
			rows, ok := assignment.Lhs[0].(*ast.Ident)
			rowsBound = ok && rows.Name == "rows" && objectAssignments(function, rows.Obj) == 1
		}
		return true
	})
	return scanned && !written && rowsBound && captured != nil && !addressEscapes(function, captured, scanAddresses)
}

func approvedMigrationExecution(source parsedSource, call *ast.CallExpr, sources []parsedSource) bool {
	if source.path != maintenanceSQLite+"migrate.go" || astShape(call.Fun) != "tx.Exec" || len(call.Args) != 1 {
		return false
	}
	function := containingFunction(source.file, call.Pos())
	if function == nil {
		return false
	}
	if function.Name.Name == "migrateVersion" {
		if astShape(call.Args[0]) == "string(script)" {
			return embeddedMigrationProgram(sources)
		}
		if astShape(call.Args[0]) != "index.SQL" || identifierCount(function, "index") != 2 || !capturedMigrationSchema(sources) {
			return false
		}
		bound := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			loop, ok := node.(*ast.RangeStmt)
			if ok && astShape(loop.Value) == "index" && astShape(loop.X) == "v27Before.SelectionIndexes" && loop.Body.Pos() < call.Pos() && call.End() < loop.Body.End() {
				bound = true
			}
			return true
		})
		return bound
	}
	drop := function.Name.Name == "dropMigration20DependentTriggers"
	if !drop && function.Name.Name != "restoreMigration20DependentTriggers" {
		return false
	}
	if astShape(function.Type) != "func(tx *sql.Tx, triggers []migration20Trigger) error" || identifierCount(function, "trigger") != 2 || identifierCount(function, "triggers") != 2 || !unchangedParameter(function, "tx") || !capturedMigrationSchema(sources) {
		return false
	}
	if drop {
		expression, ok := call.Args[0].(*ast.BinaryExpr)
		if !ok || expression.Op != token.ADD {
			return false
		}
		prefix, known := constantString(expression.X, source.constants, map[string]bool{})
		if !known || prefix != "DROP TRIGGER " || astShape(expression.Y) != "quoteSQLiteIdentifier(trigger.Name)" {
			return false
		}
		quote := namedFunction(source.file, "quoteSQLiteIdentifier")
		if quote == nil || sourceImport(source.file, "strings") != "strings" || astShape(quote.Body) != "{ return `\"` + strings.ReplaceAll(value, `\"`, `\"\"`) + `\"` }" {
			return false
		}
	} else if astShape(call.Args[0]) != "trigger.SQL" {
		return false
	}
	bound := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if ok && astShape(loop.Value) == "trigger" && astShape(loop.X) == "triggers" && loop.Body.Pos() < call.Pos() && call.End() < loop.Body.End() {
			bound = true
		}
		return true
	})
	if !bound {
		return false
	}
	count, references := 0, 0
	valid := true
	for _, file := range sources {
		references += identifierCount(file.file, function.Name.Name)
		ast.Inspect(file.file, func(node ast.Node) bool {
			invocation, ok := node.(*ast.CallExpr)
			if !ok || astShape(invocation.Fun) != function.Name.Name {
				return true
			}
			count++
			caller := containingFunction(file.file, invocation.Pos())
			if file.path != source.path || caller == nil || caller.Name.Name != "migrateVersion" || len(invocation.Args) != 2 || astShape(invocation.Args[0]) != "tx" || (astShape(invocation.Args[1]) != "v20Before.DependentTriggers" && astShape(invocation.Args[1]) != "v27Before.DependentTriggers") {
				valid = false
			}
			return true
		})
	}
	return valid && count == 2 && references == 3
}

func migrationFixture(t *testing.T, capture bool) map[string]string {
	t.Helper()
	var history map[string]string
	if err := json.Unmarshal(migrationHistory, &history); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range history {
		names = append(names, name)
	}
	sort.Strings(names)
	for index := range names {
		names[index] = strconv.Quote(names[index])
	}
	source := `package sqlite;import "context";import "database/sql";import "strings";import "omni-folio/services/core/migrations";var migrationNames=[...]string{` + strings.Join(names, ",") + `}
 func(s *Store)Migrate(ctx context.Context)error{for version:=current+1;version<=latestSchema;version++{script,err:=migrations.Files.ReadFile("migrations/"+migrationNames[version-1]);if err!=nil{return err};if err:=s.migrateVersion(ctx,version,script);err!=nil{return err}};return nil}
 func(s *Store)migrateVersion(ctx context.Context,version int,script []byte)error{tx,proof,err:=s.observations.MigrationStarter().Begin(ctx);`
	if capture {
		source += `var v20Before migration20Snapshot;v20Before,err=s.captureMigration20Snapshot(ctx,tx,proof);dropMigration20DependentTriggers(tx,v20Before.DependentTriggers);var v27Before migration27Snapshot;v27Before,err=s.captureMigration27Snapshot(ctx,tx,proof,false);dropMigration20DependentTriggers(tx,v27Before.DependentTriggers);`
	}
	source += `_,err=tx.Exec(string(script));`
	if capture {
		source += `restoreMigration20DependentTriggers(tx,v20Before.DependentTriggers);s.verifyMigration20Snapshot(ctx,tx,proof,v20Before);for _,index:=range v27Before.SelectionIndexes{tx.Exec(index.SQL)};restoreMigration20DependentTriggers(tx,v27Before.DependentTriggers);s.verifyMigration27Snapshot(ctx,tx,proof,v27Before);`
	}
	source += `return err}`
	if capture {
		source += `
 func(s *Store)captureMigration20Snapshot(ctx context.Context,q orderQuerier,proof Proof)(migration20Snapshot,error){return s.captureMigration20SnapshotWithForeignKeyPreflight(ctx,q,proof,true)}
 func(s *Store)captureMigration20SnapshotWithForeignKeyPreflight(ctx context.Context,q orderQuerier,proof Proof,pre bool)(migration20Snapshot,error){dependentTriggers,err:=migration20DependentTriggerInventory(ctx,q);return migration20Snapshot{DependentTriggers:dependentTriggers},err}
 func(s *Store)captureMigration27Snapshot(ctx context.Context,q orderQuerier,proof Proof,post bool)(migration27Snapshot,error){objects,indexes,triggers,err:=migration27SchemaInventory(ctx,q);return migration27Snapshot{SchemaObjects:objects,SelectionIndexes:indexes,DependentTriggers:triggers},err}
 func migration20DependentTriggerInventory(ctx context.Context,q orderQuerier)([]migration20Trigger,error){rows,err:=q.QueryContext(ctx,"SELECT name,tbl_name,sql FROM sqlite_master WHERE type='trigger' AND tbl_name NOT IN ('execution_authority_events','strategy_selection_events','paper_performance_policy_events') AND (instr(sql, 'execution_authority_events') != 0 OR instr(sql, 'strategy_selection_events') != 0) ORDER BY name");var triggers []migration20Trigger;for rows.Next(){var trigger migration20Trigger;rows.Scan(&trigger.Name,&trigger.Table,&trigger.SQL);triggers=append(triggers,trigger)};return triggers,nil}
 func migration27SchemaInventory(ctx context.Context,q orderQuerier)([]migration27SchemaObject,[]migration27SchemaObject,[]migration20Trigger,error){rows,err:=q.QueryContext(ctx,"SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND tbl_name NOT IN ('paper_promotion_claims','paper_promotion_decisions') ORDER BY type,name");var objects,indexes []migration27SchemaObject;var triggers []migration20Trigger;for rows.Next(){var object migration27SchemaObject;rows.Scan(&object.Type,&object.Name,&object.Table,&object.SQL);if object.Table=="strategy_selection_events"{if object.Type=="index"&&object.SQL!=""{indexes=append(indexes,object)};continue};objects=append(objects,object);if object.Type=="trigger"&&strings.Contains(object.SQL,"strategy_selection_events"){triggers=append(triggers,migration20Trigger{Name:object.Name,Table:object.Table,SQL:object.SQL})}};return objects,indexes,triggers,nil}
 func dropMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{for _,trigger:=range triggers{if _,err:=tx.Exec("DROP TRIGGER "+quoteSQLiteIdentifier(trigger.Name));err!=nil{return err}};return nil}
 func restoreMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{for _,trigger:=range triggers{if _,err:=tx.Exec(trigger.SQL);err!=nil{return err}};return nil}`
		source += "\nfunc quoteSQLiteIdentifier(value string) string {return `\"` + strings.ReplaceAll(value, `\"`, `\"\"`) + `\"`}"
	}
	driver, err := os.ReadFile("../../migrations/embed.go")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"internal/" + maintenanceSQLite + "migrate.go": source, "migrations/embed.go": string(driver)}
}

func TestEmbeddedMigrationExecutionProvenance(t *testing.T) {
	for _, change := range []string{"", "bad source", "bad name", "extra caller", "modified bytes"} {
		t.Run(change, func(t *testing.T) {
			sources := migrationFixture(t, false)
			key := "internal/" + maintenanceSQLite + "migrate.go"
			switch change {
			case "bad source":
				sources["migrations/embed.go"] = strings.Replace(sources["migrations/embed.go"], "scripts.ReadFile", "os.ReadFile", 1)
			case "bad name":
				sources[key] = strings.Replace(sources[key], "001_init.sql", "evil.sql", 1)
			case "extra caller":
				sources[key] += `;func(s *Store)Unsafe(script []byte){s.migrateVersion(ctx,1,script)}`
			case "modified bytes":
				sources[key] = strings.Replace(sources[key], `_,err=tx.Exec(string(script))`, `script=append(script,extra...);_,err=tx.Exec(string(script))`, 1)
			}
			root := fixtureSources(t, sources)
			violations, err := inspectSQLSources(filepath.Join(root, "internal"))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != (change == "") {
				t.Fatalf("change %s, got %v", change, violations)
			}
		})
	}
}

func TestMigrationSchemaRestorationRequiresCapturedObjects(t *testing.T) {
	for _, change := range []string{"", "uncaptured trigger", "rewritten index", "escaped index", "different tx", "replaced querier", "escaped querier", "escaped transaction", "aliased transaction", "replaced helper transaction", "escaped helper transaction"} {
		t.Run(change, func(t *testing.T) {
			sources := migrationFixture(t, true)
			key := "internal/" + maintenanceSQLite + "migrate.go"
			switch change {
			case "uncaptured trigger":
				sources[key] += `;func unsafe(tx *sql.Tx,triggers []migration20Trigger){restoreMigration20DependentTriggers(tx,triggers)}`
			case "rewritten index":
				sources[key] = strings.Replace(sources[key], `rows.Scan(&object.Type,&object.Name,&object.Table,&object.SQL)`, `rows.Scan(&object.Type,&object.Name,&object.Table,&object.SQL);object.SQL="DELETE FROM events"`, 1)
			case "escaped index":
				sources[key] = strings.Replace(sources[key], `rows.Scan(&object.Type,&object.Name,&object.Table,&object.SQL)`, `rows.Scan(&object.Type,&object.Name,&object.Table,&object.SQL);rewrite(&object.SQL)`, 1)
			case "different tx":
				sources[key] = strings.Replace(sources[key], `s.captureMigration27Snapshot(ctx,tx,proof,false)`, `s.captureMigration27Snapshot(ctx,otherTx,proof,false)`, 1)
			case "replaced querier":
				sources[key] = strings.Replace(sources[key], `objects,indexes,triggers,err:=migration27SchemaInventory(ctx,q)`, `q=s.db;objects,indexes,triggers,err:=migration27SchemaInventory(ctx,q)`, 1)
			case "escaped querier":
				sources[key] = strings.Replace(sources[key], `objects,indexes,triggers,err:=migration27SchemaInventory(ctx,q)`, `rewrite(&q);objects,indexes,triggers,err:=migration27SchemaInventory(ctx,q)`, 1)
			case "escaped transaction":
				sources[key] = strings.Replace(sources[key], `_,err=tx.Exec(string(script))`, `rewrite(&tx);_,err=tx.Exec(string(script))`, 1)
			case "aliased transaction":
				sources[key] = strings.Replace(sources[key], `_,err=tx.Exec(string(script))`, `alias:=&tx;*alias=otherTx;_,err=tx.Exec(string(script))`, 1)
			case "replaced helper transaction":
				sources[key] = strings.Replace(sources[key], `func restoreMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{`, `func restoreMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{tx=otherTx;`, 1)
			case "escaped helper transaction":
				sources[key] = strings.Replace(sources[key], `func dropMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{`, `func dropMigration20DependentTriggers(tx *sql.Tx,triggers []migration20Trigger)error{rewrite(&tx);`, 1)
			}
			root := fixtureSources(t, sources)
			violations, err := inspectSQLSources(filepath.Join(root, "internal"))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != (change == "") {
				t.Fatalf("change %s, got %v", change, violations)
			}
		})
	}
}

func callAssignment(function *ast.FuncDecl, call *ast.CallExpr, names string) bool {
	matched := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 || assignment.Rhs[0] != call {
			return true
		}
		var actual []string
		for _, left := range assignment.Lhs {
			actual = append(actual, astShape(left))
		}
		matched = strings.Join(actual, ", ") == names
		return true
	})
	return matched
}
