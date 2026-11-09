package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"omni-folio/services/core/internal/application/ledger"

	_ "github.com/mattn/go-sqlite3"
)

func TestLedgerWriteRollsBackFailedWorkAndCommit(t *testing.T) {
	for _, failure := range []string{"callback", "commit", "cancel", "panic"} {
		t.Run(failure, func(t *testing.T) {
			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "ledger.db")+"?_foreign_keys=on")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			_, err = db.Exec(`CREATE TABLE admitted_revision (revision INTEGER PRIMARY KEY);
INSERT INTO admitted_revision VALUES (0);
CREATE TABLE ledger_meta (singleton INTEGER PRIMARY KEY,revision INTEGER REFERENCES admitted_revision DEFERRABLE INITIALLY DEFERRED,recorded_at TEXT,last_verified_at TEXT);
INSERT INTO ledger_meta VALUES(1,0,'1970-01-01T00:00:00Z',NULL);`)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := errors.New("injected callback failure")
			panicked := false
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						if recovered != expected {
							t.Fatalf("panic changed: %v", recovered)
						}
						panicked = true
					}
				}()
				err = NewLedger(db).Write(ctx, func(scope ledger.WriteScope) error {
					if err := scope.RecordAppliedRevision(ctx, 1, "2026-01-01T00:00:00Z"); err != nil {
						return err
					}
					switch failure {
					case "callback":
						return expected
					case "cancel":
						cancel()
					case "panic":
						panic(expected)
					}
					return nil
				})
			}()
			if failure == "panic" {
				if !panicked {
					t.Fatal("panic was swallowed")
				}
			} else if err == nil {
				t.Fatal("failed transaction returned success")
			}
			if failure == "callback" && !errors.Is(err, expected) {
				t.Fatalf("callback cause changed: %v", err)
			}
			probeCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			var revision int64
			if err := db.QueryRowContext(probeCtx, `SELECT revision FROM ledger_meta WHERE singleton=1`).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if revision != 0 {
				t.Fatalf("failed transaction published revision %d", revision)
			}
		})
	}
}
