package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"

	pool "omni-folio/services/core/internal/storage/sqlite"
)

func TestG38ERestoreSchemaPinsPolicyTableDefinitionAndSequence(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *sql.DB){
		"weakened policy check": func(t *testing.T, db *sql.DB) {
			driftSQLiteTableSQLForTest(t, db, "paper_performance_policy_events",
				"sample_count INTEGER NOT NULL CHECK (sample_count > 0)", "sample_count INTEGER NOT NULL")
		},
		"policy sequence is not primary key": func(t *testing.T, db *sql.DB) {
			driftSQLiteTableSQLForTest(t, db, "paper_performance_policy_events",
				"sequence INTEGER PRIMARY KEY", "sequence INTEGER NOT NULL")
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := migrationDB(t, 0)
			if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := requireOrderRestoreSchema(db); err != nil {
				t.Fatalf("baseline schema rejected: %v", err)
			}
			mutate(t, db)
			if err := requireOrderRestoreSchema(db); err == nil {
				t.Fatal("restore schema accepted weakened policy table definition")
			}
		})
	}
}

func TestG38EMigration20ReenablesAndRechecksForeignKeys(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *sql.DB){
		"success": func(t *testing.T, _ *sql.DB) {},
		"failure": func(t *testing.T, db *sql.DB) {
			if _, err := db.Exec(`DROP INDEX execution_authority_latest_idx`); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			db := migrationDB(t, 19)
			mutate(t, db)
			err := migrationStore(t, db).Migrate(context.Background())
			if name == "success" && err != nil {
				t.Fatal(err)
			}
			if name == "failure" && err == nil {
				t.Fatal("broken migration 020 was accepted")
			}
			var enabled, violations, version int
			if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			wantVersion := latestSchema
			if name == "failure" {
				wantVersion = 19
			}
			if enabled != 1 || violations != 0 || version != wantVersion {
				t.Fatalf("migration %s foreign_keys=%d violations=%d version=%d wantVersion=%d", name, enabled, violations, version, wantVersion)
			}
		})
	}
}

func TestG38EMigration20RejectsV19ForeignKeyInventoryDriftBeforeCommit(t *testing.T) {
	sourceDB := migrationDB(t, 19)
	driftSQLiteTableSQLForTest(t, sourceDB, "strategy_selection_events",
		"source_event_id TEXT REFERENCES strategy_selection_events(event_id)",
		"source_event_id TEXT REFERENCES strategy_research_evidence(result_sha256)")
	var path string
	if err := sourceDB.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := pool.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrationStore(t, db).Migrate(context.Background()); err == nil {
		t.Fatal("migration 020 accepted a v19 foreign-key inventory it did not preserve")
	}
}

func TestG38EMigration20RejectsNonRebuiltV19ForeignKeyDriftBeforeScript(t *testing.T) {
	sourceDB := migrationDB(t, 19)
	var path string
	if err := sourceDB.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	driftSQLiteTableSQLForTest(t, sourceDB, "risk_reservations",
		"FOREIGN KEY (authority_event_id) REFERENCES execution_authority_events(event_id)",
		"FOREIGN KEY (authority_event_id) REFERENCES strategy_selection_events(event_id)")
	var expectedSourceSQL string
	if err := sourceDB.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='risk_reservations'`).Scan(&expectedSourceSQL); err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatal(err)
	}
	beforeSHA, beforeSize, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := pool.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrationStore(t, db).Migrate(context.Background()); err == nil {
		_ = db.Close()
		t.Fatal("migration 020 accepted a non-rebuilt v19 child foreign-key drift")
	}
	var version int
	var sourceSQL string
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='risk_reservations'`).Scan(&sourceSQL); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	afterSHA, afterSize, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != 19 || sourceSQL != expectedSourceSQL || beforeSHA != afterSHA || beforeSize != afterSize {
		t.Fatalf("failed migration changed source version=%d source_sql_equal=%t db=(%s,%d)->(%s,%d)",
			version, sourceSQL == expectedSourceSQL, beforeSHA, beforeSize, afterSHA, afterSize)
	}
}

func TestG38EMigration20PinsCanonicalV19ForeignKeyInventory(t *testing.T) {
	db := migrationDB(t, 19)
	inventory, err := migration20ForeignKeyInventory(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	const want = "665f412a571d311c990d15f579608f89d91cbd8376b97dc1fdd0dd2537e41cbd"
	if migration20CanonicalV19ForeignKeyInventorySHA256 != want {
		t.Fatalf("migration 020 canonical v19 foreign-key inventory constant=%s want=%s", migration20CanonicalV19ForeignKeyInventorySHA256, want)
	}
	if got := migration20ForeignKeyInventorySHA256(inventory); got != want {
		t.Fatalf("canonical v19 foreign-key inventory SHA-256=%s want=%s", got, want)
	}
}

func TestG38ERestoreSchemaRejectsPolicyIndexAndGuardDrift(t *testing.T) {
	for name, mutate := range map[string]string{
		"policy index":         `DROP INDEX paper_performance_policy_events_account_selection_idx`,
		"policy guard":         `DROP TRIGGER paper_performance_policy_events_state_guard`,
		"automatic halt guard": `DROP TRIGGER execution_authority_events_state_guard`,
	} {
		t.Run(name, func(t *testing.T) {
			db := migrationDB(t, 0)
			if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := requireOrderRestoreSchema(db); err != nil {
				t.Fatalf("baseline schema rejected: %v", err)
			}
			if _, err := db.Exec(mutate); err != nil {
				t.Fatal(err)
			}
			if err := requireOrderRestoreSchema(db); err == nil {
				t.Fatal("restore schema accepted G3.8E policy drift")
			}
		})
	}
}

func driftSQLiteTableSQLForTest(t testing.TB, db *sql.DB, table, old, replacement string) {
	t.Helper()
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(definition, old) {
		t.Fatalf("%s definition lacks drift target", table)
	}
	if _, err := db.Exec(`PRAGMA writable_schema=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sqlite_master SET sql=? WHERE type='table' AND name=?`, strings.Replace(definition, old, replacement, 1), table); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA writable_schema=OFF`); err != nil {
		t.Fatal(err)
	}
}

func TestMigration20PreservesExactForeignKeyInventoryThrough27(t *testing.T) {
	ctx := context.Background()
	db := migrationDB(t, 19)
	beforeForeignKeys, err := migration20ForeignKeyInventory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrationStore(t, db).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	afterForeignKeys, err := migration20ForeignKeyInventory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	expectedForeignKeys, expectedForeignKeyErr := migration20ExpectedForeignKeys(beforeForeignKeys)
	expectedForeignKeys = append(expectedForeignKeys,
		migration20ForeignKey{Child: "paper_runner_leases", Parent: "strategy_selection_events", From: "strategy_selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "paper_runner_leases", Parent: "strategy_selection_events", From: "selected_result_sha256", To: "selected_result_sha256", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "strategy_selection_events", Parent: "paper_promotion_decisions", From: "paper_promotion_attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "paper_promotion_decisions", Parent: "paper_promotion_claims", From: "attempt_id", To: "attempt_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
		migration20ForeignKey{Child: "paper_promotion_decisions", Parent: "strategy_selection_events", From: "selection_event_id", To: "event_id", OnUpdate: "NO ACTION", OnDelete: "NO ACTION", Match: "NONE"},
	)
	sort.Slice(expectedForeignKeys, func(i, j int) bool {
		return migration20ForeignKeyKey(expectedForeignKeys[i]) < migration20ForeignKeyKey(expectedForeignKeys[j])
	})

	if expectedForeignKeyErr != nil || !sameMigration20ForeignKeys(expectedForeignKeys, afterForeignKeys) {
		t.Fatalf("v19 migration changed foreign keys: expected=%+v actual=%+v err=%v", expectedForeignKeys, afterForeignKeys, expectedForeignKeyErr)
	}
	if err := verifyMigration20TemporaryNames(ctx, db, afterForeignKeys); err != nil {
		t.Fatal(err)
	}
}

func TestMigration27RejectedUnknownTriggerPreservesSelectionColumns(t *testing.T) {
	db := migrationDB(t, 26)
	if _, err := db.Exec(`CREATE TRIGGER user_strategy_selection_guard BEFORE INSERT ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'user selection guard');END`); err != nil {
		t.Fatal(err)
	}
	if err := migrationStore(t, db).Migrate(context.Background()); err == nil {
		t.Fatal("unknown selection trigger did not stop migration before DDL")
	}
	var newColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('strategy_selection_events') WHERE name='paper_promotion_attempt_id'`).Scan(&newColumn); err != nil || newColumn != 0 {
		t.Fatal("rejected migration changed selection schema", err)
	}
}
