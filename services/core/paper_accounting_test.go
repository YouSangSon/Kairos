package main

import (
	"database/sql"
	"testing"
)

func downgradePaperAccountingForTest(t testing.TB, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TRIGGER paper_accounting_sessions_no_update;
		DROP TRIGGER paper_accounting_sessions_no_delete;
		DROP TRIGGER paper_accounting_sessions_state_guard;
		DROP TABLE paper_accounting_sessions;
		DELETE FROM schema_migrations WHERE version=15`); err != nil {
		t.Fatal(err)
	}
}
