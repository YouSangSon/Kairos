package observation

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	storage "omni-folio/services/core/internal/storage/contracts"
)

func TestMigrationFactoryBindsFreshPurposeLimitedProofs(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(`CREATE TABLE order_events(value INTEGER);INSERT INTO order_events VALUES(7)`); err != nil {
		t.Fatal(err)
	}
	registry := New(db, func() time.Time { return time.Unix(1, 0).UTC() })
	statements := []Statement{{SQL: `SELECT value FROM order_events`}}
	reader, err := registry.MigrationTradingReader(statements)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := registry.TradingReader(statements)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := New(db, time.Now).MigrationTradingReader(statements)
	if err != nil {
		t.Fatal(err)
	}
	tx, proof, err := registry.MigrationStarter().Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE order_events SET value=8`); err != nil {
		t.Fatal(err)
	}
	var previous storage.ObservationRef
	var rows Rows
	var scanner Scanner
	for stage := 0; stage < 2; stage++ {
		if err := proof.Verify(context.Background(), func(ref storage.ObservationRef, cutoff time.Time) error {
			if ref == previous || cutoff.Unix() != 1 {
				t.Fatalf("proof identity or cutoff changed: %v", cutoff)
			}
			if _, err := ordinary.Resolve(ref); !errors.Is(err, ErrReference) {
				t.Fatalf("ordinary reader admitted migration proof: %v", err)
			}
			if _, err := foreign.Resolve(ref); !errors.Is(err, ErrReference) {
				t.Fatalf("foreign reader admitted migration proof: %v", err)
			}
			handle, err := reader.Resolve(ref)
			if err != nil {
				return err
			}
			var value int
			if err := handle.QueryOne(context.Background(), `SELECT value FROM order_events`).Scan(&value); err != nil {
				return err
			}
			if value != 8 {
				t.Fatalf("proof did not observe uncommitted DDL transaction: %d", value)
			}
			rows, err = handle.QueryContext(context.Background(), `SELECT value FROM order_events`)
			if err != nil {
				return err
			}
			scanner = handle.QueryOne(context.Background(), `SELECT value FROM order_events`)
			previous = ref
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.Resolve(previous); !errors.Is(err, ErrReference) {
			t.Fatalf("ended proof remained resolvable: %v", err)
		}
		if rows.Next() {
			t.Fatal("rows survived proof scope")
		}
		var value int
		if err := rows.Scan(&value); !errors.Is(err, ErrReference) {
			t.Fatalf("row scan survived proof scope: %v", err)
		}
		if err := scanner.Scan(&value); !errors.Is(err, ErrReference) {
			t.Fatalf("deferred scan survived proof scope: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := proof.Verify(context.Background(), func(storage.ObservationRef, time.Time) error { called = true; return nil }); !errors.Is(err, sql.ErrTxDone) || called {
		t.Fatalf("completed transaction admitted empty proof: %v called=%v", err, called)
	}
	if err := registry.VerifyBackup(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := reader.Resolve(ref); !errors.Is(err, ErrReference) {
			t.Fatalf("migration reader admitted backup scope: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationProofFailureInvalidatesReferenceAndLeavesRollbackToOwner(t *testing.T) {
	for _, failure := range []string{"callback", "panic", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "failure.db"))
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE order_events(value INTEGER)`); err != nil {
				t.Fatal(err)
			}
			registry := New(db, time.Now)
			reader, err := registry.MigrationTradingReader([]Statement{{SQL: `SELECT value FROM order_events`}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx, proof, err := registry.MigrationStarter().Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_events VALUES(9)`); err != nil {
				t.Fatal(err)
			}
			var saved storage.ObservationRef
			expected := errors.New("injected migration proof failure")
			panicked := false
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						panicked = true
						if recovered != expected {
							t.Fatalf("panic changed: %v", recovered)
						}
					}
				}()
				err = proof.Verify(ctx, func(ref storage.ObservationRef, _ time.Time) error {
					saved = ref
					switch failure {
					case "callback":
						return expected
					case "panic":
						panic(expected)
					case "cancel":
						cancel()
					}
					return nil
				})
			}()
			if failure == "panic" && !panicked {
				t.Fatal("proof panic was swallowed")
			}
			if failure == "callback" && !errors.Is(err, expected) {
				t.Fatalf("callback failure changed: %v", err)
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation changed: %v", err)
			}
			if _, err := reader.Resolve(saved); !errors.Is(err, ErrReference) {
				t.Fatalf("failed proof remained active: %v", err)
			}
			_ = tx.Rollback()
			var count int
			probeCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := db.QueryRowContext(probeCtx, `SELECT COUNT(*) FROM order_events`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("migration rollback left data: %d %v", count, err)
			}
		})
	}
}
