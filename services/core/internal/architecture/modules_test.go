package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureSources(t *testing.T, sources map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, source := range sources {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestModuleGateRejectsBoundaryViolations(t *testing.T) {
	cases := []struct{ name, path, source, want string }{
		{"private import", "modules/portfolio/internal/application/nested/a.go", `package nested; import _ "omni-folio/services/core/internal/modules/ledger/internal/domain"`, "private"},
		{"reverse business edge", "modules/ledger/internal/application/a.go", `package application; import _ "omni-folio/services/core/internal/modules/marketdata/contracts"`, "module dependency"},
		{"codec IO", "codec/a.go", `package codec;import _ "os"`, "impure dependency"},
		{"codec wall clock", "codec/a.go", `package codec;import "time";var now=time.Now`, "impure time.Now"},
		{"domain codec", "modules/ledger/internal/domain/a.go", `package domain;import _ "omni-folio/services/core/internal/codec"`, "layer"},
		{"ordinary migration authority", "modules/ledger/internal/adapters/sqlite/a.go", `package sqlite;func migrate(){registry.MigrationStarter()}`, "migration authority"},
		{"ordinary migration config", "modules/ledger/compose.go", `package ledger;import o "omni-folio/services/core/internal/storage/observation";type Config struct{Migration *o.MigrationStarter};func Compose(){}`, "scoped capability"},
		{"provider business import", "providers/kiwoom/a.go", `package kiwoom;import _ "omni-folio/services/core/internal/modules/trading/contracts"`, "business module"},
		{"nested domain database", "modules/ledger/internal/domain/nested/a.go", `package nested; import _ "database/sql"`, "forbidden database/sql"},
		{"nested domain clock", "modules/ledger/internal/domain/nested/a.go", `package nested; import clock "time"; var now=clock.Now`, "impure time.Now"},
		{"application context timer", "modules/ledger/internal/application/a.go", `package application;import "context";var timer=context.WithTimeout`, "creates clock"},
		{"domain stdout", "modules/ledger/internal/domain/a.go", `package domain;import "fmt";func debug(){fmt.Println("business")}`, "impure fmt"},
		{"nested application clock", "modules/ledger/internal/application/a.go", `package application; import clock "time"; var now=clock.Now`, "impure time.Now"},
		{"adapter legacy bypass", "modules/trading/internal/adapters/sqlite/a.go", `package sqlite; import _ "omni-folio/services/core/internal/application/ledger"`, "legacy"},
		{"nested application adapter", "modules/ledger/internal/application/nested/a.go", `package nested; import _ "omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"`, "layer"},
		{"domain public contract", "modules/ledger/internal/domain/a.go", `package domain; import _ "omni-folio/services/core/internal/modules/ledger/contracts"`, "layer"},
		{"application module root", "modules/ledger/internal/application/a.go", `package application; import _ "omni-folio/services/core/internal/modules/ledger"`, "layer"},
		{"contract database field", "modules/ledger/contracts/a.go", `package contracts; import "database/sql"; type Result struct { DB *sql.DB }`, "contract"},
		{"contract private alias", "modules/ledger/contracts/a.go", `package contracts; import d "omni-folio/services/core/internal/modules/ledger/internal/domain"; type Result=d.Row`, "contract"},
		{"contract iterator", "modules/ledger/contracts/a.go", `package contracts; import "iter"; type Result struct { Rows iter.Seq[int] }`, "contract"},
		{"contract observation result", "modules/ledger/contracts/a.go", `package contracts; import s "omni-folio/services/core/internal/storage/contracts"; type Reader interface{ Snapshot() s.ObservationRef }`, "escapes observation"},
		{"contract hidden observation field", "modules/ledger/contracts/a.go", `package contracts; import s "omni-folio/services/core/internal/storage/contracts"; type Data struct{ ref s.ObservationRef }`, "escapes observation"},
		{"contract context result", "modules/ledger/contracts/a.go", `package contracts;import "context";type Reader interface{Snapshot()context.Context}`, "escapes observation"},
		{"contract timer field", "modules/ledger/contracts/a.go", `package contracts;import "time";type Result struct{Timer *time.Timer}`, "escapes observation"},
		{"contract callback result", "modules/ledger/contracts/a.go", `package contracts; type Reader interface{ Snapshot() func() int }`, "escapes observation"},
		{"contract hidden empty interface", "modules/ledger/contracts/a.go", `package contracts;type Payload interface{};type Result struct{Value Payload}`, "unbounded interface"},
		{"contract custom iterator", "modules/ledger/contracts/a.go", `package contracts;type Iterator interface{Next()bool;Scan(...any)error};type Result struct{Rows Iterator}`, "generic storage"},
		{"contract callback input", "modules/ledger/contracts/a.go", `package contracts;type Writer interface{Run(func(any)error)error}`, "callback or resource"},
		{"contract global handle", "modules/ledger/contracts/a.go", `package contracts;var Handle any`, "mutable capability"},
		{"contract query escape", "modules/ledger/contracts/a.go", `package contracts; type Reader interface { Query(string) error }`, "generic storage"},
		{"unknown module", "modules/research/contracts/a.go", `package contracts; type Result string`, "unknown module"},
		{"module root business", "modules/ledger/api.go", `package ledger; func Snapshot() int { return 1 }`, "construction"},
		{"compose business function", "modules/ledger/compose.go", `package ledger; func Snapshot() int { return 1 }`, "construction"},
		{"compose private result", "modules/ledger/compose.go", `package ledger; import a "omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"; func Compose() *a.Store { return a.New() }`, "construction result"},
		{"compose business call", "modules/ledger/compose.go", `package ledger; import a "omni-folio/services/core/internal/modules/ledger/internal/application"; func Compose() { a.Snapshot() }`, "business function"},
		{"compose business condition", "modules/ledger/compose.go", `package ledger; func Compose(amount int) { if amount<0 {return} }`, "business condition"},
		{"compose loop", "modules/ledger/compose.go", `package ledger; func Compose() { for {} }`, "construction"},
		{"composition private", "composition/a.go", `package composition; import _ "omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"`, "private"},
		{"ordinary root DB injection", "modules/ledger/api.go", `package ledger; import "database/sql"; type Config struct { DB *sql.DB }`, "construction"},
		{"module dot alias", "modules/trading/internal/adapters/cli/a.go", `package cli; import . "omni-folio/services/core/internal/modules/marketdata/contracts"`, "dot import"},
		{"dispatch helper factory", "modules/trading/internal/adapters/kiwoom/a.go", `package kiwoom; func NewAnything(){NewMockDispatch()}`, "dispatch outside"},
		{"dispatch compose callback", "modules/trading/compose.go", `package trading; import k "omni-folio/services/core/internal/modules/trading/internal/adapters/kiwoom";func Compose(){factory:=k.NewMockDispatch;_ = factory}`, "constructor callback"},
		{"SDK constructor outside sealed adapter", "modules/trading/compose.go", `package trading;import sdk "omni-folio/services/core/internal/providers/kiwoom";func Compose(){sdk.NewMockOrders(nil)}`, "sealed dispatch"},
		{"SDK submit outside sealed adapter", "modules/trading/internal/application/a.go", `package application;func submit(){orders.SubmitMockLimitOrder()}`, "sealed dispatch"},
		{"SDK submit method value", "modules/trading/internal/adapters/kiwoom/dispatch.go", `package kiwoom;type MockDispatch struct{};func(MockDispatch)SubmitLimitOrder(){submit:=orders.SubmitMockLimitOrder;_ = submit}`, "sealed dispatch"},
		{"SDK constructor method value", "modules/trading/internal/adapters/kiwoom/dispatch.go", `package kiwoom;import sdk "omni-folio/services/core/internal/providers/kiwoom";func NewMockDispatch(){factory:=sdk.NewMockOrders;_ = factory}`, "sealed dispatch"},
		{"dispatch arbitrary adapter caller", "modules/trading/internal/adapters/kiwoom/a.go", `package kiwoom; func business(){NewMockDispatch()}`, "dispatch outside"},
		{"dispatch factory outside adapter", "modules/trading/internal/application/a.go", `package application; import k "omni-folio/services/core/internal/modules/trading/internal/adapters/kiwoom"; var factory=k.NewMockDispatch`, "dispatch"},
		{"dispatch arbitrary compose helper", "modules/trading/compose.go", `package trading; import k "omni-folio/services/core/internal/modules/trading/internal/adapters/kiwoom"; func Helper(){ k.NewMockDispatch() }`, "dispatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureSources(t, map[string]string{tc.path: tc.source})
			violations, _, err := inspectDependencies(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(violations, "\n"), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, violations)
			}
		})
	}
}

func TestModuleGateAllowsPrivateInwardComposition(t *testing.T) {
	root := fixtureSources(t, map[string]string{
		"modules/ledger/contracts/errors.go":                   `package contracts;import "errors";var ErrMissing=errors.New("missing")`,
		"modules/ledger/contracts/api.go":                      `package contracts; import "context"; type Snapshot struct { Revision int64 }; type Reader interface { Snapshot(context.Context) (Snapshot,error) }`,
		"modules/marketdata/contracts/read.go":                 `package contracts; import "context"; import s "omni-folio/services/core/internal/storage/contracts"; type Reader interface { Read(context.Context,s.ObservationRef) (string,error) }`,
		"modules/trading/internal/adapters/kiwoom/dispatch.go": `package kiwoom;import sdk "omni-folio/services/core/internal/providers/kiwoom";type MockDispatch struct{};func NewMockDispatch(){sdk.NewMockOrders(nil)};func(MockDispatch)SubmitLimitOrder(){orders.SubmitMockLimitOrder()}`,
		"modules/trading/internal/adapters/kiwoom/factory.go":  `package kiwoom; type FactoryConfig struct{}`,
		"modules/ledger/internal/domain/nested/value.go":       `package nested; import "omni-folio/services/core/internal/exact"; var _=exact.Parse`,
		"modules/portfolio/internal/application/read.go":       `package application; import l "omni-folio/services/core/internal/modules/ledger/contracts"; type Ledger interface { Snapshot() l.Snapshot }`,
		"modules/ledger/compose.go":                            `package ledger; import "database/sql"; import a "omni-folio/services/core/internal/modules/ledger/internal/adapters/sqlite"; import "omni-folio/services/core/internal/modules/ledger/contracts"; type Config struct { DB *sql.DB }; func Compose(c Config) contracts.Reader { return a.New(c.DB) }`,
		"modules/marketdata/compose.go":                        `package marketdata;import "database/sql";import "errors";import "omni-folio/services/core/internal/storage/observation";import s "omni-folio/services/core/internal/modules/marketdata/internal/adapters/sqlite";import a "omni-folio/services/core/internal/modules/marketdata/internal/application";import c "omni-folio/services/core/internal/modules/marketdata/contracts";type Config struct{DB *sql.DB;Observations *observation.Registry};func Compose(config Config) c.Reader {if !config.Observations.MatchesDB(config.DB)||config.Observations==nil{return nil};reader,err:=config.Observations.MarketReader(s.ReadStatements());if err!=nil{_ = errors.New("bad");return nil};return a.New(reader)};func ComposeOwnerPrice(atomic c.Reader)c.Reader{return a.New(atomic)}`,
		"composition/core.go":                                  `package composition; import l "omni-folio/services/core/internal/modules/ledger"; var _=l.Compose`,
	})
	violations, _, err := inspectDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatal(violations)
	}
}

func TestModuleGateDetectsCompletePackageCycles(t *testing.T) {
	root := fixtureSources(t, map[string]string{
		"modules/marketdata/contracts/a.go": `package contracts; import _ "omni-folio/services/core/internal/modules/ledger/contracts"`,
		"modules/ledger/contracts/a.go":     `package contracts; import _ "omni-folio/services/core/internal/modules/marketdata/contracts"`,
	})
	violations, _, err := inspectDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(violations, "\n"), "import cycle") {
		t.Fatal(violations)
	}
}

func TestModuleCycleIncludesImportsFromDifferentLayers(t *testing.T) {
	root := fixtureSources(t, map[string]string{
		"modules/ledger/internal/application/a.go":     `package application; import _ "omni-folio/services/core/internal/modules/marketdata/contracts"`,
		"modules/marketdata/internal/application/a.go": `package application; import _ "omni-folio/services/core/internal/modules/ledger/contracts"`,
	})
	violations, _, err := inspectDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(violations, "\n"), "import cycle module:") {
		t.Fatal(violations)
	}
}
