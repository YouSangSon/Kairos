package architecture

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

const candleCheckpointAdapter = "modules/marketdata/internal/adapters/sqlite/candle_checkpoint.go"

// Keep the reviewed SQL independent of the adapter so a statement change requires a gate update.
const (
	checkpointCatalog   = `SELECT name, type, sql FROM main.sqlite_schema ORDER BY name LIMIT 2`
	checkpointIdentity  = `PRAGMA application_id = 1262699344`
	checkpointHasPages  = `SELECT EXISTS(SELECT 1 FROM candle_capture_pages)`
	checkpointReadPages = `SELECT page_number, symbol, interval, started_at, digest, complete,
		CASE WHEN page_number = (SELECT max(page_number) FROM candle_capture_pages)
			AND length(payload) BETWEEN 1 AND 1048576 THEN payload END,
		page_sha256
		FROM candle_capture_pages ORDER BY page_number LIMIT 33`
	checkpointAppend = `INSERT INTO candle_capture_pages
		(page_number, symbol, interval, started_at, digest, complete, payload, page_sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	checkpointDDL = `CREATE TABLE candle_capture_pages (
	page_number INTEGER PRIMARY KEY CHECK (page_number BETWEEN 1 AND 32),
	symbol TEXT NOT NULL CHECK (length(symbol) = 6 AND symbol NOT GLOB '*[^0-9]*'),
	interval TEXT NOT NULL CHECK (length(interval) BETWEEN 2 AND 3),
	started_at TEXT NOT NULL CHECK (length(started_at) BETWEEN 20 AND 30),
	digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
	complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
	payload BLOB NOT NULL CHECK (length(payload) BETWEEN 1 AND 1048576),
	page_sha256 TEXT NOT NULL CHECK (length(page_sha256) = 64 AND page_sha256 NOT GLOB '*[^0-9a-f]*')
) STRICT`
)

func approvedCandleCheckpointSQL(source parsedSource, call *ast.CallExpr, method string) bool {
	if len(call.Args) < 2 || call.Ellipsis.IsValid() {
		return false
	}
	query, known := constantString(call.Args[1], source.constants, map[string]bool{})
	if !known {
		return false
	}
	switch query {
	case checkpointCatalog, checkpointReadPages:
		return method == "QueryContext" && len(call.Args) == 2
	case checkpointHasPages:
		return method == "QueryRowContext" && len(call.Args) == 2
	case checkpointAppend:
		return method == "ExecContext" && len(call.Args) == 10
	case checkpointDDL, checkpointIdentity:
		if method != "ExecContext" || len(call.Args) != 2 {
			return false
		}
		for _, decl := range source.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "createCandleCheckpointSchema" && fn.Recv == nil && fn.Body != nil && fn.Body.Pos() < call.Pos() && call.End() < fn.Body.End() {
				return true
			}
		}
	}
	return false
}

func TestCandleCheckpointSQLSourceProfile(t *testing.T) {
	for _, tc := range []struct {
		name, method, query string
		args                int
		allow               bool
	}{
		{"read pages", "QueryContext", checkpointReadPages, 0, true},
		{"page existence", "QueryRowContext", checkpointHasPages, 0, true},
		{"append page", "ExecContext", checkpointAppend, 8, true},
		{"bounded metadata", "QueryContext", checkpointCatalog, 0, true},
		{"canonical market data", "QueryContext", `SELECT * FROM security_price_observations`, 0, false},
		{"canonical schema version", "QueryContext", `SELECT version FROM schema_migrations`, 0, false},
		{"foreign join", "QueryContext", `SELECT * FROM candle_capture_pages JOIN events ON 1=1`, 0, false},
		{"foreign subquery", "QueryContext", `SELECT * FROM candle_capture_pages WHERE EXISTS (SELECT 1 FROM events)`, 0, false},
		{"attached checkpoint", "QueryContext", `SELECT * FROM other.candle_capture_pages`, 0, false},
		{"unbounded metadata", "QueryContext", strings.TrimSuffix(checkpointCatalog, " LIMIT 2"), 0, false},
		{"wrong method", "ExecContext", checkpointCatalog, 0, false},
		{"wrong argument count", "QueryRowContext", checkpointHasPages, 1, false},
		{"mutate committed page", "ExecContext", `UPDATE candle_capture_pages SET complete=1`, 0, false},
		{"mutable pragma", "ExecContext", `PRAGMA journal_mode=WAL`, 0, false},
		{"multi-statement", "QueryRowContext", checkpointHasPages + "; DELETE FROM events", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("package sqlite; func run(){db.%s(ctx, %q%s)}", tc.method, tc.query, strings.Repeat(", arg", tc.args))
			violations, err := inspectSQLSources(fixtureSources(t, map[string]string{candleCheckpointAdapter: source}))
			if err != nil {
				t.Fatal(err)
			}
			if (len(violations) == 0) != tc.allow {
				t.Fatalf("allow %t, got %v", tc.allow, violations)
			}
		})
	}
}

func TestCandleCheckpointSQLInitialization(t *testing.T) {
	for _, tc := range []struct {
		name, function, ddl, identity string
		violations                    int
	}{
		{"initialize", "createCandleCheckpointSchema", checkpointDDL, checkpointIdentity, 0},
		{"outside initialization", "run", checkpointDDL, checkpointIdentity, 2},
		{"additional table", "createCandleCheckpointSchema", checkpointDDL + "; CREATE TABLE another (n INT)", checkpointIdentity, 1},
		{"wrong identity", "createCandleCheckpointSchema", checkpointDDL, `PRAGMA application_id = 42`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("package sqlite; func %s(){tx.ExecContext(ctx, %q); tx.ExecContext(ctx, %q)}", tc.function, tc.ddl, tc.identity)
			violations, err := inspectSQLSources(fixtureSources(t, map[string]string{candleCheckpointAdapter: source}))
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != tc.violations {
				t.Fatalf("want %d violations, got %v", tc.violations, violations)
			}
		})
	}
}

func TestCandleCheckpointSQLDeniedOutsideAdapter(t *testing.T) {
	for _, path := range []string{
		"modules/marketdata/internal/adapters/sqlite/store.go",
		"modules/marketdata/internal/adapters/sqlite/sub/candle_checkpoint.go",
		"modules/maintenance/internal/adapters/sqlite/candle_checkpoint.go",
	} {
		t.Run(path, func(t *testing.T) {
			source := fmt.Sprintf("package sqlite; func run(){db.QueryRowContext(ctx, %q)}", checkpointHasPages)
			violations, err := inspectSQLSources(fixtureSources(t, map[string]string{path: source}))
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) == 0 {
				t.Fatal("accepted checkpoint SQL outside exact adapter")
			}
		})
	}
}

func TestCandleCheckpointCannotRegisterOrForwardSQL(t *testing.T) {
	sources := map[string]string{
		"statement registration": `package sqlite
import o "omni-folio/services/core/internal/storage/observation"
var stmt = o.Statement{SQL: "SELECT * FROM security_price_observations"}`,
		"dynamic forwarder": `package sqlite
import "context"
type checkpoint struct{ db interface{ QueryContext(context.Context, string, ...any) (any, error) } }
func (c checkpoint) QueryContext(ctx context.Context, query string, args ...any) (any, error) {
	return c.db.QueryContext(ctx, query, args...)
}`,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			violations, err := inspectSQLSources(fixtureSources(t, map[string]string{candleCheckpointAdapter: source}))
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) == 0 {
				t.Fatal("accepted unscoped SQL authority")
			}
		})
	}
}
