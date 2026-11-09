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
		{"catalog JOIN target", `SELECT m.sql FROM security_price_observations p JOIN sqlite_master m ON m.name='events'`},
		{"catalog comma target", `SELECT m.sql FROM security_price_observations p, sqlite_master m WHERE m.name='events'`},
		{"catalog JOIN source", `SELECT m.sql FROM sqlite_master m JOIN security_price_observations p ON m.name='security_price_observations'`},
		{"catalog predicate bypass", `SELECT sql FROM sqlite_master WHERE name='security_price_observations' OR 1=1`},
		{"pragma list missing scope", `SELECT strict FROM pragma_table_list WHERE name='security_price_observations'`},
		{"pragma list temp scope", `SELECT strict FROM pragma_table_list WHERE schema='temp' AND type='table' AND name='security_price_observations'`},
		{"pragma list dynamic object", `SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name=?`},
		{"pragma list foreign object", `SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='events'`},
		{"pragma list broad OR", `SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='security_price_observations' OR name='security_price_observations'`},
		{"foreign fixed pragma table function", `SELECT name FROM pragma_table_info('events')`},
		{"foreign fixed pragma index function", `SELECT name FROM pragma_index_info('events_idx')`},
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
		{"trading", `SELECT strict FROM pragma_table_list WHERE schema='main' AND type='table' AND name='paper_runner_leases'`},
		{"marketdata", `SELECT name FROM pragma_table_info('security_price_observations')`},
		{"marketdata", `SELECT name FROM pragma_index_info('owner_price_series_latest_idx')`},
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
		{"local const foreign", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; func read(){const query=`SELECT * FROM events`; db.Query(query)}", "owned by ledger"},
		{"shadowed SQL const", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; const query=`SELECT * FROM security_price_observations`; func read(query string){db.Query(query)}", "unresolved SQL"},
		{"method value", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; func read(query string){run:=db.Query; run(query)}", "method value"},
		{"comment prefixed SQL", "modules/marketdata/internal/adapters/sqlite/a.go", "package sqlite; func read(){db.Query(`/*bounded*/ SELECT * FROM events`)}", "owned by ledger"},
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
		`CREATE TRIGGER events_guard AFTER INSERT ON events BEGIN SELECT * FROM events, order_events; END`,
		`CREATE TRIGGER events_guard AFTER INSERT ON events BEGIN SELECT 1 WHERE 1 IN order_events; END`,
		`CREATE TABLE events(id INTEGER REFERENCES order_events)`,
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

func TestSQLStatementMetadataRequiresEveryObjectArgument(t *testing.T) {
	source := `package sqlite
 import o "omni-folio/services/core/internal/storage/observation"
 const query="SELECT name FROM sqlite_master WHERE name=? OR tbl_name=?"
 var statements=[]o.Statement{{SQL:query,Arguments:2,Objects:[]string{"watchlist_state"},ObjectArguments:[]int{0}}}
 func read(){db.Query(query,"watchlist_state","watchlist_state")}`
	root := fixtureSources(t, map[string]string{"modules/watchlist/internal/adapters/sqlite/a.go": source})
	violations, err := inspectSQLSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(violations, "\n"), "does not cover") {
		t.Fatal(violations)
	}
	root = fixtureSources(t, map[string]string{"modules/watchlist/internal/adapters/sqlite/a.go": strings.Replace(source, "[]int{0}", "[]int{0,1}", 1)})
	violations, err = inspectSQLSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatal(violations)
	}
}

func TestCatalogGateRejectsJoinedForeignSchema(t *testing.T) {
	for _, query := range []string{
		`SELECT m.sql FROM watchlist_state AS w JOIN sqlite_master AS m ON m.name = 'events'`,
		`SELECT m.sql FROM watchlist_state AS w, sqlite_master AS m WHERE m.name = 'events'`,
	} {
		if err := ownership.ValidateSQL("watchlist", query, nil); err == nil {
			t.Fatalf("accepted foreign catalog query %s", query)
		}
	}
}

func TestSQLGateRejectsQuotedKeywordAliases(t *testing.T) {
	for _, alias := range []string{"where", "group", "order"} {
		query := `SELECT o.* FROM events AS "` + alias + `", order_events AS o`
		if err := ownership.ValidateSQL("ledger", query, nil); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
}
func TestSQLGateRejectsUnknownScalarFunctions(t *testing.T) {
	for _, query := range []string{`SELECT custom_write(?) FROM events`, `SELECT made_up(value) FROM events`} {
		if err := ownership.ValidateSQL("ledger", query, nil); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
}

func TestJSONTableFunctionKeepsOwnershipListContext(t *testing.T) {
	schema := `CREATE TRIGGER order_events_guard AFTER INSERT ON order_events BEGIN SELECT 1 FROM json_each(NEW.event_json) AS j, events AS e; END;`
	if _, err := ownership.ValidateSchema(schema); err == nil {
		t.Error("accepted foreign table after trigger json_each")
	}
	query := `SELECT (SELECT e.event_id FROM json_each(new.event_json) AS j, events AS e LIMIT 1) FROM order_events AS new`
	if err := ownership.ValidateSQL("trading", query, nil); err == nil {
		t.Error("accepted foreign table after correlated json_each")
	}
}
func TestSQLGateRejectsNonAnonymousParameters(t *testing.T) {
	for _, query := range []string{
		`SELECT ?2, sql FROM sqlite_master WHERE name=?`,
		`SELECT sql FROM sqlite_master WHERE name=:name`,
		`SELECT sql FROM sqlite_master WHERE name=@name`,
		`SELECT sql FROM sqlite_master WHERE name=$name`,
	} {
		if err := ownership.ValidateSQL("watchlist", query, []string{"watchlist_state"}); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
}
func TestOwnerPriceWriteRequiresExactCurrentTarget(t *testing.T) {
	for _, query := range []string{
		`INSERT INTO owner_fx_series_versions(version) VALUES(?) /* INTO owner_price_series_versions */`,
		`INSERT INTO owner_price_series_versions_new(version) VALUES(?)`,
		`INSERT INTO owner_price_series_versions(version) SELECT version FROM owner_fx_series_versions`,
	} {
		if err := ownership.ValidateOwnerPriceWrite(query); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
	if err := ownership.ValidateOwnerPriceWrite(`INSERT INTO owner_price_series_versions(version) VALUES(?)`); err != nil {
		t.Fatal(err)
	}
}

func TestSQLGateRejectsEmptyOrQuotedUnknownFunction(t *testing.T) {
	for _, query := range []string{`SELECT ""() FROM events`, `SELECT "123"() FROM events`} {
		if err := ownership.ValidateSQL("ledger", query, nil); err == nil {
			t.Errorf("accepted %s", query)
		}
	}
}

func TestSQLGateRecognizesOnlyTransparentPrivateForwarders(t *testing.T) {
	source := `package sqlite;import "context";type raw interface{QueryContext(context.Context,string,...any)(any,error)};type reader struct{q raw};func(r reader)QueryContext(ctx context.Context,query string,args ...any)(any,error){return r.q.QueryContext(ctx,query,args...)};func read(q reader){q.QueryContext(context.Background(),"SELECT event_id FROM events")}`
	root := fixtureSources(t, map[string]string{"modules/ledger/internal/adapters/sqlite/a.go": source})
	violations, err := inspectSQLSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatal(violations)
	}
	source = strings.Replace(source, "r.q.QueryContext(ctx,query,args...)", "r.q.QueryContext(ctx,query+extra,args...)", 1)
	root = fixtureSources(t, map[string]string{"modules/ledger/internal/adapters/sqlite/a.go": source})
	violations, err = inspectSQLSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(violations, "\n"), "unresolved SQL") {
		t.Fatal(violations)
	}
}

func TestMigrationLivenessProbeIsExact(t *testing.T) {
	source := `package observation;type MigrationProof struct{};func(p *MigrationProof)Verify(){p.tx.QueryRowContext(ctx,"SELECT 1")}`
	root := fixtureSources(t, map[string]string{"storage/observation/migration.go": source})
	violations, err := inspectSQLSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatal(violations)
	}
	for _, bad := range []string{strings.Replace(source, "SELECT 1", "SELECT * FROM events", 1), strings.Replace(source, "Verify", "Other", 1)} {
		root = fixtureSources(t, map[string]string{"storage/observation/migration.go": bad})
		violations, err = inspectSQLSources(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) == 0 {
			t.Fatal("accepted broadened migration probe")
		}
	}
}
