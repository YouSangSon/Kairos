package architecture

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
	"testing"
)

func astShape(node ast.Node) string {
	var output bytes.Buffer
	if format.Node(&output, token.NewFileSet(), node) != nil {
		return ""
	}
	return strings.Join(strings.Fields(output.String()), " ")
}

func maintenanceConstructionFile(path string) bool {
	return path == "modules/maintenance/compose.go" || path == "modules/maintenance/compose_verification.go"
}

func approvedOwnersMapper(path string, file *ast.File, imports map[string]string, literal *ast.FuncLit) bool {
	if !maintenanceConstructionFile(path) || imports["sql"] != "database/sql" || imports["sqlite"] != internalPrefix+"modules/maintenance/internal/adapters/sqlite" {
		return false
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || literal.Pos() < function.Pos() || literal.End() > function.End() {
			continue
		}
		entry, config := "Compose", "Config"
		if strings.HasSuffix(path, "compose_verification.go") {
			entry, config = "ComposeVerification", "VerificationConfig"
		}
		if function.Name.Name != entry || function.Type.Params == nil || len(function.Type.Params.List) != 1 || len(function.Type.Params.List[0].Names) != 1 || function.Type.Params.List[0].Names[0].Name != "config" || astShape(function.Type.Params.List[0].Type) != config {
			return false
		}
		return astShape(literal) == `func(db *sql.DB) (sqlite.Owners, error) { owners, err := config.NewOwners(db) return sqlite.Owners{Observations: owners.Observations, Ledger: owners.Ledger, Market: owners.Market, Trading: owners.Trading, Watchlist: owners.Watchlist}, err }`
	}
	return false
}

func manifestVerifierResult(function *ast.FuncDecl, imports map[string]string) bool {
	if function.Type.Results == nil || len(function.Type.Results.List) != 2 {
		return false
	}
	selector, ok := function.Type.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && imports[pkg.Name] == internalPrefix+"modules/maintenance/contracts" && selector.Sel.Name == "ManifestVerifier" && astShape(function.Type.Results.List[1].Type) == "error"
}

func approvedPromotionCompletion(path string, file *ast.File, imports map[string]string, selector *ast.SelectorExpr) bool {
	if path != "modules/trading/internal/application/promotion.go" || imports["context"] != "context" || imports["time"] != "time" {
		return false
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "PromotePaperCandidate" || function.Recv == nil || len(function.Recv.List) != 1 || len(function.Recv.List[0].Names) != 1 || function.Recv.List[0].Names[0].Name != "s" || astShape(function.Recv.List[0].Type) != "*UseCases" || function.Body == nil {
			continue
		}
		for index, statement := range function.Body.List {
			if selector.Pos() < statement.Pos() || selector.End() > statement.End() || index+2 >= len(function.Body.List) {
				continue
			}
			block := &ast.BlockStmt{List: function.Body.List[index : index+3]}
			if astShape(block) != `{ finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second) defer cancel() receipt, err := s.FinishPaperPromotion(finishCtx, *claim, comparison, scoreErr, false) }` {
				return false
			}
			count := 0
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok && ident.Name == "finishCtx" {
					count++
				}
				return true
			})
			return count == 2
		}
	}
	return false
}

func TestExactConstructionExceptions(t *testing.T) {
	mapper := `func(db *sql.DB) (sqlite.Owners,error){owners,err:=config.NewOwners(db);return sqlite.Owners{Observations:owners.Observations,Ledger:owners.Ledger,Market:owners.Market,Trading:owners.Trading,Watchlist:owners.Watchlist},err}`
	source := `package maintenance;import "database/sql";import sqlite "omni-folio/services/core/internal/modules/maintenance/internal/adapters/sqlite";func Compose(config Config){factory:=` + mapper + `;_ = factory}`
	cases := []struct {
		name, source string
		reject       bool
	}{
		{"exact mapper", source, false},
		{"extra business call", strings.Replace(source, "owners,err:=", "business();owners,err:=", 1), true},
		{"changed error", strings.Replace(source, ",err}", ",nil}", 1), true},
		{"swapped capability", strings.Replace(source, "Ledger:owners.Ledger", "Ledger:owners.Market", 1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/maintenance/compose.go": tc.source}))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) > 0) != tc.reject {
				t.Fatalf("reject %t, got %v", tc.reject, violations)
			}
		})
	}
	verification := strings.Replace(source, "func Compose(config Config)", "func ComposeVerification(config VerificationConfig)(c.ManifestVerifier,error)", 1)
	verification = strings.Replace(verification, ";func ComposeVerification", `;import c "omni-folio/services/core/internal/modules/maintenance/contracts";func ComposeVerification`, 1)
	verification = strings.Replace(verification, "_ = factory}", "_ = factory;return nil,nil}", 1)
	for _, wide := range []bool{false, true} {
		current := verification
		if wide {
			current = strings.Replace(current, "c.ManifestVerifier", "c.Backups", 1)
		}
		violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/maintenance/compose_verification.go": current}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(violations, "\n"), "only ManifestVerifier") != wide {
			t.Fatalf("wide %t, got %v", wide, violations)
		}
	}
}

func TestBoundedPromotionCompletionContext(t *testing.T) {
	source := `package application;import "context";import "time";func(s *UseCases)PromotePaperCandidate(ctx context.Context){finishCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),10*time.Second);defer cancel();receipt,err:=s.FinishPaperPromotion(finishCtx,*claim,comparison,scoreErr,false);_,_=receipt,err}`
	for _, change := range []string{"", "duration", "cancel", "escape", "other method"} {
		current := source
		switch change {
		case "duration":
			current = strings.Replace(current, "10*time.Second", "11*time.Second", 1)
		case "cancel":
			current = strings.Replace(current, "defer cancel();", "", 1)
		case "escape":
			current = strings.Replace(current, "_,_=receipt,err", "sink(finishCtx);_,_=receipt,err", 1)
		case "other method":
			current = strings.Replace(current, "PromotePaperCandidate", "Other", 1)
		}
		violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/trading/internal/application/promotion.go": current}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(violations, "\n"), "creates clock") != (change != "") {
			t.Fatalf("change %s, got %v", change, violations)
		}
	}
}

func TestSchemaComparisonDataHasNoSQLAuthority(t *testing.T) {
	for _, execute := range []bool{false, true} {
		source := `package sqlite;import "strings";func proof(schema string){_ = strings.Index(schema,"CREATE TABLE events")}`
		if execute {
			source = `package sqlite;func proof(){db.Exec("CREATE TABLE events")}`
		}
		violations, err := inspectSQLSources(fixtureSources(t, map[string]string{"modules/maintenance/internal/adapters/sqlite/restore_schema.go": source}))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) > 0) != execute {
			t.Fatalf("execute %t, got %v", execute, violations)
		}
	}
}
