package architecture

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
)

func tradingConstructionCondition(path, entry string, expr ast.Expr) bool {
	if constructionCondition(expr) {
		return true
	}
	if path != "modules/trading/compose.go" || entry != "Compose" {
		return false
	}
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if binary.Op == token.LOR || binary.Op == token.LAND {
		return tradingConstructionCondition(path, entry, binary.X) && tradingConstructionCondition(path, entry, binary.Y)
	}
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return false
	}
	empty, known := constantString(binary.Y, nil, map[string]bool{})
	if !known || empty != "" {
		return false
	}
	selector, ok := binary.X.(*ast.SelectorExpr)
	return ok && astShape(selector.X) == "config" && (selector.Sel.Name == "ExecutionOwner" || selector.Sel.Name == "RunnerOwner")
}

func tradingConstructionBinding(path, entry string, file *ast.File, call *ast.CallExpr) bool {
	if path != "modules/trading/compose.go" || entry != "Compose" || len(call.Args) != 1 || sourceImport(file, "sqlite") != internalPrefix+"modules/trading/internal/adapters/sqlite" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	store, ok := selector.X.(*ast.Ident)
	if !ok || store.Name != "store" || store.Obj == nil {
		return false
	}
	binding, ok := store.Obj.Decl.(*ast.AssignStmt)
	if !ok || len(binding.Lhs) != 1 || len(binding.Rhs) != 1 {
		return false
	}
	constructor, ok := binding.Rhs[0].(*ast.CallExpr)
	if !ok || astShape(constructor.Fun) != "sqlite.New" {
		return false
	}
	function := containingFunction(file, call.Pos())
	if function == nil || objectAssignments(function, store.Obj) != 1 {
		return false
	}
	switch selector.Sel.Name {
	case "BindRecoveryReaders":
		return astShape(call.Args[0]) == "config.Observations"
	case "BindDispatcher":
		if sourceImport(file, "broker") != internalPrefix+"modules/trading/internal/adapters/kiwoom" {
			return false
		}
		dispatch, ok := call.Args[0].(*ast.Ident)
		if !ok || dispatch.Obj == nil || objectAssignments(function, dispatch.Obj) != 1 {
			return false
		}
		assignment, ok := dispatch.Obj.Decl.(*ast.AssignStmt)
		return ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 && astShape(assignment.Rhs[0]) == "broker.NewMockDispatch(config.MockBrokerClient)"
	}
	return false
}

func TestTradingConstructionBindingsStayLocal(t *testing.T) {
	source := `package trading;import sqlite "omni-folio/services/core/internal/modules/trading/internal/adapters/sqlite";import broker "omni-folio/services/core/internal/modules/trading/internal/adapters/kiwoom";func Compose(config Config){if config.ExecutionOwner==""||config.RunnerOwner==""{return};store:=sqlite.New(config.DB);store.BindRecoveryReaders(config.Observations);dispatch,err:=broker.NewMockDispatch(config.MockBrokerClient);_ = err;store.BindDispatcher(dispatch)}`
	for _, change := range []string{"", "business condition", "foreign store", "foreign reader", "foreign dispatch", "other method"} {
		current := source
		switch change {
		case "business condition":
			current = strings.Replace(current, "config.ExecutionOwner", "config.Balance", 1)
		case "foreign store":
			current = strings.Replace(current, "store:=sqlite.New(config.DB)", "store:=config.Store", 1)
		case "foreign reader":
			current = strings.Replace(current, "BindRecoveryReaders(config.Observations)", "BindRecoveryReaders(other)", 1)
		case "foreign dispatch":
			current = strings.Replace(current, "broker.NewMockDispatch(config.MockBrokerClient)", "broker.NewOther(config.MockBrokerClient)", 1)
		case "other method":
			current = strings.Replace(current, "BindDispatcher(dispatch)", "BindBusiness(dispatch)", 1)
		}
		violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/trading/compose.go": current}))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != (change == "") {
			t.Fatalf("change %s, got %v", change, violations)
		}
	}
}
