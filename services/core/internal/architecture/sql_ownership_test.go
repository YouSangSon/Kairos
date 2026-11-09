package architecture

import (
	"fmt"
	"omni-folio/services/core/internal/storage/ownership"
	"reflect"
	"strings"
	"testing"
)

func TestSQLGateRejectsOwnershipAndUnsupportedForms(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"foreign select", `SELECT * FROM events`},
		{"foreign join", `SELECT p.* FROM security_price_observations AS p JOIN events AS e ON e.instrument_id=p.instrument_id`},
		{"foreign alias", `SELECT x.* FROM main.events x`},
		{"foreign comma", `SELECT * FROM security_price_observations p, events e`},
		{"parenthesized foreign table", `SELECT * FROM (order_events), security_price_observations`},
		{"IN foreign table", `SELECT instrument_id FROM security_price_observations WHERE instrument_id IN events`},
		{"unregistered prefix table", `SELECT * FROM security_price_observations_secret`},
		{"write function", `SELECT load_extension(?) FROM security_price_observations`},
		{"catalog wrong qualifier", `SELECT sql FROM sqlite_master sm WHERE another.name='security_price_observations'`},
		{"catalog unbounded union", `SELECT name FROM sqlite_master WHERE name='security_price_observations' UNION SELECT name FROM sqlite_master`},
		{"foreign nested", `SELECT * FROM security_price_observations WHERE EXISTS (SELECT 1 FROM events)`},
		{"cte", `WITH e AS (SELECT * FROM events) SELECT * FROM e`},
		{"multiple", `SELECT * FROM security_price_observations; DELETE FROM events`},
		{"table parameter", `SELECT * FROM ?`},
		{"table function", `SELECT * FROM pragma_table_info(?)`},
		{"unknown table", `SELECT * FROM new_prices`},
		{"unknown view", `CREATE VIEW new_prices AS SELECT * FROM security_price_observations`},
		{"unknown trigger", `CREATE TRIGGER surprise AFTER INSERT ON security_price_observations BEGIN DELETE FROM events; END`},
		{"unbounded catalog", `SELECT sql FROM sqlite_master`},
		{"catalog parameter", `SELECT sql FROM sqlite_master WHERE name=?`},
		{"foreign catalog", `SELECT sql FROM sqlite_temp_master WHERE name='events'`},
		{"catalog predicate bypass", `SELECT sql FROM sqlite_master WHERE name='security_price_observations' OR 1=1`},
		{"foreign pragma", `PRAGMA table_info(events)`},
		{"attached database", `SELECT * FROM other.security_price_observations`},
		{"control write", `UPDATE schema_migrations SET version=1`},
		{"maintenance ordinary write", `DELETE FROM security_price_observations`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := "marketdata"
			if tc.name == "maintenance ordinary write" {
				owner = "maintenance"
			}
			if err := checkOwnedSQL(owner, tc.sql, nil); err == nil {
				t.Fatalf("accepted %s", tc.sql)
			}
		})
	}
}

func TestSQLGateAcceptsFixedOwnerStatements(t *testing.T) {
	cases := []struct{ owner, sql string }{
		{"ledger", `SELECT e.event_id FROM events e LEFT JOIN receipts r ON e.receipt_id=r.receipt_id WHERE e.sequence<=?`},
		{"marketdata", `SELECT * FROM security_price_observations p WHERE EXISTS (SELECT 1 FROM instrument_listing_events l WHERE l.instrument_id=p.instrument_id)`},
		{"marketdata", `INSERT INTO owner_price_series_versions(version) VALUES(?)`},
		{"marketdata", `SELECT sql FROM sqlite_master WHERE name='security_price_observations'`},
		{"marketdata", `SELECT name FROM sqlite_temp_master WHERE tbl_name IN ('owner_price_series_versions','security_price_observations')`},
		{"watchlist", `SELECT COUNT(*) FROM (SELECT name FROM sqlite_master WHERE tbl_name='watchlist_state' AND type IN ('index','trigger') UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name='watchlist_state' OR name='watchlist_state')`},
		{"marketdata", `PRAGMA table_info(security_price_observations)`},
		{"ledger", `SELECT version FROM schema_migrations`},
		{"maintenance", `INSERT INTO schema_migrations(version) VALUES(?)`},
	}
	for _, tc := range cases {
		t.Run(tc.owner+"/"+tc.sql, func(t *testing.T) {
			if err := checkOwnedSQL(tc.owner, tc.sql, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := checkOwnedSQL("marketdata", `SELECT sql FROM sqlite_master WHERE name=?`, []string{"security_price_observations"}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLSourceGateRejectsDynamicAndWrongLocation(t *testing.T) {
	cases := []struct{ name, path, source, want string }{
		{"fixed foreign", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; func read(){ db.Query(`SELECT * FROM events`) }", "owned by ledger"},
		{"concatenated const foreign", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; const table=`events`; const query=`SELECT * FROM `+table; func read(){ db.Query(query) }", "owned by ledger"},
		{"dynamic table", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; func read(table string){ db.Query(`SELECT * FROM `+table) }", "unresolved SQL"},
		{"dynamic variable", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; var statement=`SELECT * FROM security_price_observations`; func read(){ db.Query(statement) }", "unresolved SQL"},
		{"application SQL", "modules/ledger/internal/application/a.go", "package application; func read(){ db.Query(`SELECT * FROM events`) }", "private sqlite adapter"},
		{"composition SQL", "composition/a.go", "package composition; func read(){ db.Query(`SELECT * FROM events`) }", "private sqlite adapter"},
		{"hidden sql literal", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; var statement=`SELECT * FROM events`", "owned by ledger"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureSources(t, map[string]string{tc.path: tc.source})
			violations, err := inspectSQLSources(root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(violations, "\n"), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, violations)
			}
		})
	}
}

func TestSchemaInventoryMatchesRegistry(t *testing.T) {
	violations, err := inspectSchemaInventory("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestSchemaGateRejectsUnownedObjects(t *testing.T) {
	for _, sql := range []string{
		`CREATE TABLE mystery(id INTEGER)`,
		`CREATE VIEW mystery AS SELECT * FROM events`,
		`CREATE TRIGGER mystery AFTER INSERT ON events BEGIN SELECT 1; END`,
		`CREATE TRIGGER events_guard AFTER INSERT ON events BEGIN DELETE FROM order_events; END`,
	} {
		t.Run(fmt.Sprint(sql), func(t *testing.T) {
			if err := checkSchemaSQL(sql); err == nil {
				t.Fatalf("accepted %s", sql)
			}
		})
	}
}

func TestCatalogObjectArgumentPositions(t *testing.T) {
	query := `SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name='watchlist_state'`
	if err := ownership.ValidateSQL("watchlist", query, []string{"watchlist_state"}); err != nil {
		t.Fatal(err)
	}
	indexes, err := ownership.CatalogArguments(query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(indexes, []int{0}) {
		t.Fatalf("got object arguments %v", indexes)
	}
	query = `SELECT name FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger') UNION ALL SELECT name FROM sqlite_temp_master WHERE tbl_name=? AND type IN ('index','trigger')`
	indexes, err = ownership.CatalogArguments(query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(indexes, []int{0, 1}) {
		t.Fatalf("got object arguments %v", indexes)
	}
}
