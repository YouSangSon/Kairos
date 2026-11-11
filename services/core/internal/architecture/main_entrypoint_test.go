package architecture

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func thinMainViolations(file *ast.File) []string {
	var violations []string
	if file.Name.Name != "main" {
		violations = append(violations, "main entrypoint has wrong package")
	}
	allowed := map[string]bool{"context": true, "errors": true, "log": true, "os": true, "os/signal": true, "syscall": true, internalPrefix + "composition": true}
	for _, item := range file.Imports {
		path, _ := strconv.Unquote(item.Path.Value)
		if !allowed[path] {
			violations = append(violations, "main entrypoint imports non-composition implementation "+path)
		}
	}
	if sourceImport(file, "composition") != internalPrefix+"composition" {
		violations = append(violations, "main entrypoint must delegate to composition")
	}
	functions := map[string]bool{}
	for _, decl := range file.Decls {
		if group, ok := decl.(*ast.GenDecl); ok {
			if group.Tok == token.IMPORT {
				continue
			}
			if group.Tok != token.VAR || len(group.Specs) != 1 {
				violations = append(violations, "main entrypoint permits only opaque CLI options state")
				continue
			}
			value, ok := group.Specs[0].(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "cliOptions" || len(value.Values) != 0 || astShape(value.Type) != "composition.CLIOptions" {
				violations = append(violations, "main entrypoint permits only opaque CLI options state")
			}
			continue
		}
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Body == nil {
			violations = append(violations, "main entrypoint contains implementation declaration")
			continue
		}
		functions[function.Name.Name] = true
		switch function.Name.Name {
		case "run":
			if astShape(function.Type) != "func(args []string) error" || astShape(function.Body) != "{ return runContext(context.Background(), args) }" {
				violations = append(violations, "main run wrapper contains implementation")
			}
		case "runContext":
			if astShape(function.Type) != "func(ctx context.Context, args []string) error" || astShape(function.Body) != "{ return composition.RunCLI(ctx, args, cliOptions, os.Stdin, os.Stdout, os.Stderr) }" {
				violations = append(violations, "main context wrapper contains implementation")
			}
		case "main":
			if astShape(function.Type) != "func()" {
				violations = append(violations, "main entrypoint has unexpected signature")
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch node.(type) {
				case *ast.FuncLit, *ast.ForStmt, *ast.RangeStmt, *ast.GoStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
					violations = append(violations, "main entrypoint contains non-wrapper control flow")
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := astShape(call.Fun)
				switch callee {
				case "len", "run", "runContext", "context.Background", "signal.NotifyContext", "errors.Is", "log.Print", "os.Exit":
				case "stop":
					identifier, ok := call.Fun.(*ast.Ident)
					valid := false
					if ok && identifier.Obj != nil {
						if assignment, ok := identifier.Obj.Decl.(*ast.AssignStmt); ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 {
							producer, ok := assignment.Rhs[0].(*ast.CallExpr)
							valid = ok && astShape(producer.Fun) == "signal.NotifyContext"
						}
					}
					if !valid {
						violations = append(violations, "main entrypoint calls arbitrary cancellation callback")
					}
				default:
					violations = append(violations, "main entrypoint calls implementation "+callee)
				}
				return true
			})
		default:
			violations = append(violations, "main entrypoint contains implementation function "+function.Name.Name)
		}
	}
	if len(functions) != 3 || !functions["main"] || !functions["run"] || !functions["runContext"] {
		violations = append(violations, "main entrypoint requires three fixed wrappers")
	}
	if identifierCount(file, "cliOptions") != 2 {
		violations = append(violations, "main entrypoint mutates or escapes opaque CLI options")
	}
	return violations
}

func approvedLedgerCLIFactory(path string, file *ast.File, literal *ast.FuncLit) bool {
	if path != "modules/ledger/run_cli.go" || sourceImport(file, "c") != internalPrefix+"modules/ledger/contracts" || sourceImport(file, "app") != internalPrefix+"modules/ledger/internal/application" || sourceImport(file, "provider") != internalPrefix+"modules/ledger/internal/adapters/provider" || sourceImport(file, "cli") != internalPrefix+"modules/ledger/internal/adapters/cli" {
		return false
	}
	function := containingFunction(file, literal.Pos())
	if function == nil || function.Name.Name != "RunCLI" || len(function.Body.List) != 2 || astShape(literal) != "func(broker c.Broker) app.MockSync { return app.MockSync{Broker: broker, Factory: factory} }" {
		return false
	}
	if astShape(function.Body.List[0]) != "factory := provider.MockSnapshots{ReadSecret: config.ReadSecret, Transport: config.Transport, Now: config.Now, Pacer: config.Pacer}" {
		return false
	}
	result, ok := function.Body.List[1].(*ast.ReturnStmt)
	if !ok || len(result.Results) != 1 {
		return false
	}
	call, ok := result.Results[0].(*ast.CallExpr)
	return ok && astShape(call.Fun) == "cli.Run" && len(call.Args) == 8 && call.Args[6] == literal && astShape(call.Args[0]) == "ctx" && astShape(call.Args[1]) == "command" && astShape(call.Args[2]) == "args" && astShape(call.Args[3]) == "config.Resources" && astShape(call.Args[4]) == "out" && astShape(call.Args[5]) == "config.Stderr" && astShape(call.Args[7]) == "files.ExportPublisher{}" && sourceImport(file, "files") == internalPrefix+"modules/ledger/internal/adapters/files"
}

func TestThinMainMustDelegateToComposition(t *testing.T) {
	source := `package main;import "context";import "omni-folio/services/core/internal/composition";import "os";var cliOptions composition.CLIOptions;func main(){run(os.Args[1:])};func run(args []string)error{return runContext(context.Background(),args)};func runContext(ctx context.Context,args []string)error{return composition.RunCLI(ctx,args,cliOptions,os.Stdin,os.Stdout,os.Stderr)}`
	for _, change := range []string{"", "module root", "business function", "SQL callback", "mutated options"} {
		current := source
		switch change {
		case "module root":
			current = strings.Replace(current, `import "context"`, `import "context";import _ "omni-folio/services/core/internal/modules/ledger"`, 1)
		case "business function":
			current += `;func Snapshot(){}`
		case "SQL callback":
			current = strings.Replace(current, "run(os.Args[1:])", "db.Query(\"SELECT * FROM events\")", 1)
		case "mutated options":
			current = strings.Replace(current, "run(os.Args[1:])", "cliOptions.MockReadRuntime=factory;run(os.Args[1:])", 1)
		}
		root := fixtureSources(t, map[string]string{"root/main.go": current})
		violations, _, err := inspectDependencies(root)
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != (change == "") {
			t.Fatalf("change %s, got %v", change, violations)
		}
		legacyRoot := fixtureSources(t, map[string]string{"main.go": current})
		legacy, err := legacySymbols(legacyRoot)
		if err != nil {
			t.Fatal(err)
		}
		_, retained := legacy["main.go"]
		if retained != (change != "") {
			t.Fatalf("main legacy exemption mismatch for %s", change)
		}
	}
}

func TestLedgerCLIResourceFactoryIsConstructionOnly(t *testing.T) {
	source := `package ledger;import "context";import c "omni-folio/services/core/internal/modules/ledger/contracts";import app "omni-folio/services/core/internal/modules/ledger/internal/application";import provider "omni-folio/services/core/internal/modules/ledger/internal/adapters/provider";import cli "omni-folio/services/core/internal/modules/ledger/internal/adapters/cli";import files "omni-folio/services/core/internal/modules/ledger/internal/adapters/files";func RunCLI(ctx context.Context,command string,args []string,config CLIConfig,out Writer)error{factory:=provider.MockSnapshots{ReadSecret:config.ReadSecret,Transport:config.Transport,Now:config.Now,Pacer:config.Pacer};return cli.Run(ctx,command,args,config.Resources,out,config.Stderr,func(broker c.Broker)app.MockSync{return app.MockSync{Broker:broker,Factory:factory}},files.ExportPublisher{})}`
	for _, change := range []string{"", "callback business call", "other factory", "other invocation", "other file"} {
		current, path := source, "modules/ledger/run_cli.go"
		switch change {
		case "callback business call":
			current = strings.Replace(current, "return app.MockSync", "broker.Sync();return app.MockSync", 1)
		case "other factory":
			current = strings.Replace(current, "Factory:factory", "Factory:other", 1)
		case "other invocation":
			current = strings.Replace(current, "return cli.Run", "return app.Run", 1)
		case "other file":
			path = "modules/ledger/cli.go"
		}
		violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{path: current}))
		if err != nil {
			t.Fatal(err)
		}
		if (len(violations) == 0) != (change == "") {
			t.Fatalf("change %s, got %v", change, violations)
		}
	}
}
