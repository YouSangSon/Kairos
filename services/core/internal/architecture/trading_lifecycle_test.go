package architecture

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
)

var tradingTimeoutSeconds = map[string]string{"paperRunnerLoopReleaseTimeout": "5", "paperScheduledRunnerReleaseTimeout": "5", "paperScheduledRunnerContentionTimeout": "5", "paperRunnerStageTimeout": "20"}

func timeoutConstant(value ast.Expr, seconds string, file *ast.File) bool {
	binary, ok := value.(*ast.BinaryExpr)
	if !ok || binary.Op != token.MUL {
		return false
	}
	literal, ok := binary.X.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT || literal.Value != seconds {
		return false
	}
	selector, ok := binary.Y.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Second" && astShape(selector.X) == "time" && sourceImport(file, "time") == "time"
}
func tradingTimeoutDeclarations(file *ast.File) []string {
	var violations []string
	found := map[string]bool{}
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			for index, name := range value.Names {
				seconds, required := tradingTimeoutSeconds[name.Name]
				if !required {
					continue
				}
				found[name.Name] = true
				if index >= len(value.Values) || !timeoutConstant(value.Values[index], seconds, file) {
					violations = append(violations, "trading lifecycle timeout bound changed "+name.Name)
				}
			}
		}
	}
	for name := range tradingTimeoutSeconds {
		if !found[name] {
			violations = append(violations, "trading lifecycle timeout bound missing "+name)
		}
	}
	return violations
}
func approvedTradingTimeout(path string, file *ast.File, selector *ast.SelectorExpr) bool {
	if sourceImport(file, "context") != "context" || selector.Sel.Name != "WithTimeout" {
		return false
	}
	function := containingFunction(file, selector.Pos())
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	expected, base, contextName := "", "ctx", "stageCtx"
	switch path {
	case "modules/trading/internal/application/local.go":
		if function.Name.Name != "Close" || astShape(function.Recv.List[0].Type) != "*LocalPaperRun" {
			return false
		}
		expected, base, contextName = "paperRunnerLoopReleaseTimeout", "context.Background()", "ctx"
	case "modules/trading/internal/application/scheduled.go":
		if astShape(function.Recv.List[0].Type) != "*UseCases" {
			return false
		}
		switch function.Name.Name {
		case "RunDuePaperPerformancePolicy":
			expected, base, contextName = "paperScheduledRunnerReleaseTimeout", "context.Background()", "releaseCtx"
		case "WaitForPaperRunnerLeaseOrCompletion":
			expected, contextName = "paperScheduledRunnerContentionTimeout", "waitCtx"
		case "EvaluatePaperPerformanceWithClaim", "EvaluatePaperStrategyPerformanceWithClaim", "ApplyPaperPerformancePolicyWithClaim":
			expected = "paperRunnerStageTimeout"
		case "RunPaperPerformanceLoopWithWait":
			expected, base, contextName = "paperRunnerLoopReleaseTimeout", "context.Background()", "cleanupCtx"
		default:
			return false
		}
	default:
		return false
	}
	bound := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for index, statement := range block.List {
			assignment, ok := statement.(*ast.AssignStmt)
			if !ok || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 || index+1 >= len(block.List) {
				continue
			}
			call, ok := assignment.Rhs[0].(*ast.CallExpr)
			if !ok || call.Fun != selector || len(call.Args) != 2 || astShape(call.Args[0]) != base || astShape(assignment.Lhs[0]) != contextName {
				continue
			}
			duration, ok := call.Args[1].(*ast.Ident)
			if !ok || duration.Name != expected {
				continue
			}
			if duration.Obj != nil {
				value, ok := duration.Obj.Decl.(*ast.ValueSpec)
				if !ok || duration.Obj.Kind != ast.Con || len(value.Values) != 1 || !timeoutConstant(value.Values[0], tradingTimeoutSeconds[expected], file) {
					continue
				}
			}
			cancel, ok := assignment.Lhs[1].(*ast.Ident)
			if !ok {
				continue
			}
			var cancellation *ast.CallExpr
			if deferred, ok := block.List[index+1].(*ast.DeferStmt); ok {
				cancellation = deferred.Call
			} else if path == "modules/trading/internal/application/local.go" && index+3 == len(block.List) {
				cleanup := astShape(block.List[index+1])
				if cleanup == "err = r.Service.HaltOwnedSyntheticExecutionLease(ctx, r.Account, r.Lease.FencingToken)" || cleanup == "err = errors.Join(err, r.Service.ReleasePaperRunnerLease(ctx, r.Claim))" && sourceImport(file, "errors") == "errors" {
					if statement, ok := block.List[index+2].(*ast.ExprStmt); ok {
						cancellation, _ = statement.X.(*ast.CallExpr)
					}
				}
			}
			if cancellation == nil || len(cancellation.Args) != 0 {
				continue
			}
			name, ok := cancellation.Fun.(*ast.Ident)
			if ok && name.Obj == cancel.Obj {
				bound = true
			}
		}
		return true
	})
	return bound
}

func TestLocalCleanupCancelsAfterExactOwnedOperation(t *testing.T) {
	for _, operation := range []string{"err = r.Service.HaltOwnedSyntheticExecutionLease(ctx, r.Account, r.Lease.FencingToken)", "err = errors.Join(err, r.Service.ReleasePaperRunnerLease(ctx, r.Claim))"} {
		for _, change := range []string{"", "missing cancel", "intervening return", "foreign operation", "changed duration"} {
			source := `package application;import "context";import "time";import "errors";const paperRunnerLoopReleaseTimeout=5*time.Second;func(r *LocalPaperRun)Close()error{var err error;if r.Lease!=nil{ctx,cancel:=context.WithTimeout(context.Background(),paperRunnerLoopReleaseTimeout);` + operation + `;cancel()};return err}`
			switch change {
			case "missing cancel":
				source = strings.Replace(source, ";cancel()", "", 1)
			case "intervening return":
				source = strings.Replace(source, ";cancel()", ";return err;cancel()", 1)
			case "foreign operation":
				source = strings.Replace(source, operation, "err = r.Service.Other(ctx)", 1)
			case "changed duration":
				source = strings.Replace(source, "5*time.Second", "6*time.Second", 1)
			}
			violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/trading/internal/application/local.go": source}))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != (change == "") {
				t.Fatalf("operation %s, change %s, got %v", operation, change, violations)
			}
		}
	}
}

func approvedJSONEOF(path string, file *ast.File, selector *ast.SelectorExpr) bool {
	if path != "modules/trading/internal/application/promotion_rules.go" && path != "codec/record.go" || selector.Sel.Name != "EOF" || sourceImport(file, "errors") != "errors" {
		return false
	}
	function := containingFunction(file, selector.Pos())
	if function == nil || function.Name.Name != "EnsureJSONEOF" {
		return false
	}
	allowed := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && astShape(call.Fun) == "errors.Is" && len(call.Args) == 2 && call.Args[1] == selector {
			allowed = true
		}
		return true
	})
	return allowed
}

func TestTradingLifecycleTimeoutsKeepTheirBounds(t *testing.T) {
	source := `package application;import "context";import "time";const paperRunnerStageTimeout=20*time.Second;func(s *UseCases)EvaluatePaperPerformanceWithClaim(ctx context.Context){stageCtx,cancel:=context.WithTimeout(ctx,paperRunnerStageTimeout);defer cancel();s.execute(stageCtx)}`
	for _, change := range []string{"", "duration", "cancel", "function value", "other method", "unbounded argument"} {
		current := source
		switch change {
		case "duration":
			current = strings.Replace(current, "20*time.Second", "21*time.Second", 1)
		case "cancel":
			current = strings.Replace(current, "defer cancel();", "", 1)
		case "function value":
			current = strings.Replace(current, "stageCtx,cancel:=context.WithTimeout(ctx,paperRunnerStageTimeout)", "timeout:=context.WithTimeout;stageCtx,cancel:=timeout(ctx,paperRunnerStageTimeout)", 1)
		case "other method":
			current = strings.Replace(current, "EvaluatePaperPerformanceWithClaim", "Other", 1)
		case "unbounded argument":
			current = strings.Replace(current, "context.WithTimeout(ctx,paperRunnerStageTimeout)", "context.WithTimeout(ctx,delay)", 1)
		}
		violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{"modules/trading/internal/application/local.go": strings.Replace(current, "EvaluatePaperPerformanceWithClaim", "Other", 1), "modules/trading/internal/application/scheduled.go": current + `;const paperRunnerLoopReleaseTimeout=5*time.Second;const paperScheduledRunnerReleaseTimeout=5*time.Second;const paperScheduledRunnerContentionTimeout=5*time.Second`}))
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(violations, "\n")
		if change == "" {
			for _, violation := range violations {
				if !strings.HasPrefix(violation, "modules/trading/internal/application/local.go") {
					t.Fatal(violations)
				}
			}
		} else if !strings.Contains(joined, "scheduled.go") {
			t.Fatalf("change %s, got %v", change, violations)
		}
	}
}
func TestJSONEOFSentinelCannotExposeIO(t *testing.T) {
	for _, path := range []string{"modules/trading/internal/application/promotion_rules.go", "codec/record.go"} {
		for _, expression := range []string{"errors.Is(err,io.EOF)", "io.ReadAll(reader)", "io.Reader"} {
			source := `package application;import "io";import "errors";func EnsureJSONEOF(){_ = ` + expression + `}`
			violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{path: source}))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != (expression == "errors.Is(err,io.EOF)") {
				t.Fatalf("%s, got %v", expression, violations)
			}
		}
	}
}
