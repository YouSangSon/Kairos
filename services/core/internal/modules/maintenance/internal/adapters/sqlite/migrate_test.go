package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	trading "omni-folio/services/core/internal/modules/trading"
	tradingcontracts "omni-folio/services/core/internal/modules/trading/contracts"
	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	"omni-folio/services/core/migrations"
)

func migrationDB(t testing.TB, schema int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "migration.db")+"?_busy_timeout=5000&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for version := 1; version <= schema; version++ {
		script, err := migrations.Files.ReadFile(migrationNames[version-1])
		if err != nil {
			t.Fatal(err)
		}
		disable := version == 7 || version == 20 || version == 27
		if disable {
			if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
				t.Fatal(err)
			}
		}
		var dependent []migration20Trigger
		if version == 20 {
			dependent, err = migration20DependentTriggerInventory(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if version == 20 {
			if err := dropMigration20DependentTriggers(tx, dependent); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if _, err := tx.Exec(string(script)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if version == 20 {
			if err := restoreMigration20DependentTriggers(tx, dependent); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,applied_at)VALUES(?,?)`, version, "2026-01-10T00:00:00Z"); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if disable {
			if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func migrationStore(t testing.TB, db *sql.DB) *Store {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC) }
	registry := observation.New(db, now)
	sequence := 0
	owner, err := trading.Compose(trading.Config{DB: db, Observations: registry, Clock: now, ID: func(prefix string) string { sequence++; return fmt.Sprintf("%s_%d", prefix, sequence) }, ExecutionOwner: "migration-test-execution", RunnerOwner: "migration-test-runner"})
	if err != nil {
		t.Fatal(err)
	}
	return New(db, registry, owner, now, nil, nil)
}

func assertMigratedState(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	if err := requireSchemaVersionContext(context.Background(), db, version); err != nil {
		t.Fatal(err)
	}
	var enabled, violations int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || violations != 0 {
		t.Fatalf("foreign_keys=%d violations=%d", enabled, violations)
	}
}

func TestMigrateEmptyDatabaseThroughOwnedProofs(t *testing.T) {
	db := migrationDB(t, 0)
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMigratedState(t, db, 27)
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil || tables != 33 {
		t.Fatalf("canonical table inventory=%d err=%v", tables, err)
	}
}

func TestMigration20PreservesCanonicalForeignKeysThrough27(t *testing.T) {
	db := migrationDB(t, 19)
	before, err := migration20ForeignKeyInventory(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	const want = "665f412a571d311c990d15f579608f89d91cbd8376b97dc1fdd0dd2537e41cbd"
	if migration20CanonicalV19ForeignKeyInventorySHA256 != want || migration20ForeignKeyInventorySHA256(before) != want {
		t.Fatal("canonical v19 foreign-key inventory changed")
	}
	if err := migrationStore(t, db).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMigratedState(t, db, 27)
	if err := verifyMigration20TemporaryNames(context.Background(), db, before); err != nil {
		t.Fatal(err)
	}
}

func TestMigration27FailureRestoresTriggersAndForeignKeys(t *testing.T) {
	db := migrationDB(t, 26)
	if _, err := db.Exec(`CREATE TABLE paper_promotion_claims(blocked INTEGER) STRICT`); err != nil {
		t.Fatal(err)
	}
	before, indexes, triggers, err := migration27SchemaInventory(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrationStore(t, db).Migrate(context.Background()); err == nil {
		t.Fatal("colliding promotion schema was accepted")
	}
	after, restoredIndexes, restored, err := migration27SchemaInventory(context.Background(), db)
	if err != nil || !sameMigration27SchemaObjects(before, after) || !sameMigration27SchemaObjects(indexes, restoredIndexes) || !sameMigration20Triggers(triggers, restored) {
		t.Fatal("failed migration did not restore existing schema", err)
	}
	assertMigratedState(t, db, 26)
	var newColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('strategy_selection_events') WHERE name='paper_promotion_attempt_id'`).Scan(&newColumn); err != nil || newColumn != 0 {
		t.Fatal("failed migration changed selection schema", err)
	}
}

func TestMigration27RejectsUnknownSelectionTriggerBeforeDDL(t *testing.T) {
	db := migrationDB(t, 26)
	const triggerName = "user_strategy_selection_guard"
	if _, err := db.Exec(`CREATE TRIGGER user_strategy_selection_guard BEFORE INSERT ON strategy_selection_events BEGIN SELECT RAISE(ABORT,'user selection guard');END`); err != nil {
		t.Fatal(err)
	}
	var beforeSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, triggerName).Scan(&beforeSQL); err != nil {
		t.Fatal(err)
	}
	if err := migrationStore(t, db).Migrate(context.Background()); err == nil || !strings.Contains(err.Error(), "migration 27 pre-rebuild proof") || !strings.Contains(err.Error(), triggerName) {
		t.Fatal("unknown selection trigger did not stop migration before DDL", err)
	}
	var afterSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, triggerName).Scan(&afterSQL); err != nil || beforeSQL != afterSQL {
		t.Fatal("rejected migration changed user trigger", err)
	}
	assertMigratedState(t, db, 26)
	var promotionTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('paper_promotion_claims','paper_promotion_decisions')`).Scan(&promotionTables); err != nil || promotionTables != 0 {
		t.Fatal("rejected migration created promotion tables", err)
	}
}

type panickingMigrationRecovery struct {
	tradingcontracts.Recovery
	failure error
}

func (p panickingMigrationRecovery) MigrationFacts(context.Context, storage.ObservationRef, tradingcontracts.MigrationQuery) (*tradingcontracts.MigrationSnapshot, error) {
	panic(p.failure)
}
func TestMigrationProofPanicRollsBackAndRestoresForeignKeys(t *testing.T) {
	db := migrationDB(t, 19)
	store := migrationStore(t, db)
	expected := errors.New("injected owner proof panic")
	store.trading = panickingMigrationRecovery{Recovery: store.trading, failure: expected}
	panicked := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicked = true
				if recovered != expected {
					t.Fatal("owner proof panic changed", recovered)
				}
			}
		}()
		_ = store.Migrate(context.Background())
	}()
	if !panicked {
		t.Fatal("owner proof panic was swallowed")
	}
	assertMigratedState(t, db, 19)
}

func TestG38EMigration20UsesFixedCreateCopyDropRenameOrder(t *testing.T) {
	script, err := migrations.Files.ReadFile("migrations/020_paper_performance_policy_events.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(script)
	createAuthority := strings.Index(sql, "CREATE TABLE execution_authority_events_new")
	copyAuthority := strings.Index(sql, "INSERT INTO execution_authority_events_new")
	dropAuthority := strings.Index(sql, "DROP TABLE execution_authority_events;")
	renameAuthority := strings.Index(sql, "ALTER TABLE execution_authority_events_new RENAME TO execution_authority_events;")
	createPolicy := strings.Index(sql, "CREATE TABLE paper_performance_policy_events")
	if createAuthority < 0 || copyAuthority <= createAuthority || dropAuthority <= copyAuthority ||
		renameAuthority <= dropAuthority || createPolicy <= renameAuthority ||
		strings.Contains(sql, "RENAME TO execution_authority_events_old") ||
		strings.Contains(sql, "RENAME TO strategy_selection_events_old") {
		t.Fatal("migration 020 does not use the authoritative create/copy/drop/rename rebuild order")
	}
}
