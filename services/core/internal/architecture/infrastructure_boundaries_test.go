package architecture

import (
	"strings"
	"testing"
)

func TestMockReadSDKReferencesStayPrivate(t *testing.T) {
	for _, symbol := range []string{"ReadMockSecret", "DecodeMockCredentials", "NewMockSnapshot", "NewMockCandles", "NewMockReadPacer", "WaitMockRead"} {
		for _, path := range []string{"modules/ledger/internal/adapters/http/a.go", "modules/trading/internal/adapters/provider/a.go", "modules/marketdata/internal/application/a.go", "composition/core.go"} {
			for _, expression := range []string{"sdk." + symbol + "(nil)", "sdk." + symbol} {
				t.Run(path+symbol+expression, func(t *testing.T) {
					source := `package adapter;import sdk "omni-folio/services/core/internal/providers/kiwoom";func use(){_ = ` + expression + `}`
					violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{path: source}))
					if err != nil {
						t.Fatal(err)
					}
					want := !strings.HasPrefix(path, "composition/")
					if strings.Contains(strings.Join(violations, "\n"), "mock read ") != want {
						t.Fatalf("forbidden=%t, got %v", want, violations)
					}
				})
			}
		}
	}
}

func TestOuterInfrastructureBoundaries(t *testing.T) {
	cases := []struct{ path, source, want string }{
		{"modules/ledger/internal/domain/a.go", `package domain;import _ "omni-folio/services/core/internal/ledgerdomain"`, "forbidden"},
		{"modules/marketdata/internal/adapters/provider/a.go", `package provider;import sdk "omni-folio/services/core/internal/providers/kiwoom";var pacer=sdk.NewMockReadPacer`, "pacer outside process composition"},
		{"modules/portfolio/internal/adapters/cli/a.go", `package cli;import _ "omni-folio/services/core/internal/modules/ledger/construction"`, "CLI construction"},
		{"modules/ledger/contracts/a.go", `package contracts;import _ "omni-folio/services/core/internal/modules/ledger/construction"`, "CLI construction"},
		{"modules/ledger/construction/cli.go", `package construction;type Session interface{Ledger()func()int}`, "private resource"},
		{"modules/ledger/construction/cli.go", `package construction;type Session interface{Ledger(any)}`, "private resource"},

		{"files/private/a.go", `package privatefile;import _ "omni-folio/services/core/internal/modules/ledger/contracts"`, "business module"},
		{"files/private/a.go", `package privatefile;import _ "database/sql"`, "storage authority"},
		{"modules/ledger/internal/application/a.go", `package application;import _ "omni-folio/services/core/internal/files/private"`, "forbidden"},
		{"modules/ledger/internal/adapters/http/a.go", `package http;import _ "omni-folio/services/core/internal/modules/ledger/construction"`, "CLI construction"},
		{"modules/ledger/construction/cli.go", `package construction;import _ "os"`, "CLI construction"},
		{"modules/ledger/construction/cli.go", `package construction;func Open(){}`, "CLI construction"},
		{"modules/ledger/construction/cli.go", `package construction;import "database/sql";type Session interface{DB()*sql.DB}`, "CLI construction"},
		{"modules/ledger/compose_identifiers.go", `package ledger;import d "omni-folio/services/core/internal/modules/ledger/internal/domain";func ComposeIdentifiers()func(string)string{return d.Unsafe}`, "identifier construction"},
	}
	for _, tc := range cases {
		t.Run(tc.path+tc.source, func(t *testing.T) {
			violations, _, err := inspectDependencies(fixtureSources(t, map[string]string{tc.path: tc.source}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(violations, "\n"), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, violations)
			}
		})
	}
	for _, path := range []string{"files/private/a.go", "modules/ledger/construction/cli.go"} {
		violations, err := inspectSQLSources(fixtureSources(t, map[string]string{path: `package infrastructure;const query="SELECT * FROM events"`}))
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) == 0 {
			t.Errorf("SQL ignored in %s", path)
		}
	}
}

func TestAllowedOuterInfrastructure(t *testing.T) {
	sources := map[string]string{
		"modules/ledger/compose_identifiers.go":              `package ledger;import d "omni-folio/services/core/internal/modules/ledger/internal/domain";func ComposeIdentifiers()func(string)string{return d.InstrumentIDForSymbol}`,
		"modules/ledger/internal/adapters/cli/a.go":          `package cli;import _ "omni-folio/services/core/internal/files/private";import _ "omni-folio/services/core/internal/modules/ledger/construction"`,
		"modules/marketdata/internal/adapters/provider/a.go": `package provider;import sdk "omni-folio/services/core/internal/providers/kiwoom";func use(){_ = sdk.ReadMockSecret;_ = sdk.DecodeMockCredentials;_ = sdk.NewMockCandles}`,
		"modules/ledger/internal/adapters/provider/a.go":     `package provider;import sdk "omni-folio/services/core/internal/providers/kiwoom";func use(){_ = sdk.NewMockSnapshot}`,
		"composition/core.go":                                `package composition;import _ "omni-folio/services/core/internal/files/private";import _ "omni-folio/services/core/internal/modules/ledger/construction"`,
		"modules/ledger/construction/cli.go":                 `package construction;import "context";type Session interface{Close()error};type CLIResources interface{OpenReadOnly(context.Context,string)(Session,error)}`,
	}
	violations, _, err := inspectDependencies(fixtureSources(t, sources))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatal(violations)
	}
}
