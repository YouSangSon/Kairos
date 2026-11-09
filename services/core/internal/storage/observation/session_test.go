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

func TestObservationLifetimeAndAuthority(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE events(value INTEGER); INSERT INTO events VALUES(7)`); err != nil {
		t.Fatal(err)
	}
	registry := New(db, func() time.Time { return time.Unix(1, 0).UTC() })
	reader, err := registry.LedgerReader([]Statement{{SQL: `SELECT value FROM events`, Arguments: 0}})
	if err != nil {
		t.Fatal(err)
	}
	var saved storage.ObservationRef
	err = registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, cutoff time.Time) error {
		saved = ref
		if cutoff.Unix() != 1 {
			t.Fatalf("unexpected cutoff %v", cutoff)
		}
		handle, err := reader.Resolve(ref)
		if err != nil {
			return err
		}
		var value int
		if err := handle.QueryOne(context.Background(), `SELECT value FROM events`).Scan(&value); err != nil {
			return err
		}
		if value != 7 {
			t.Fatalf("value = %d", value)
		}
		if _, err := handle.QueryContext(context.Background(), `DELETE FROM events RETURNING value`); !errors.Is(err, ErrStatement) {
			t.Fatalf("write query accepted: %v", err)
		}
		if _, err := handle.QueryContext(context.Background(), `SELECT value FROM events`, 1); !errors.Is(err, ErrStatement) {
			t.Fatalf("argument mismatch accepted: %v", err)
		}
		other := New(db, time.Now)
		otherReader, err := other.LedgerReader([]Statement{{SQL: `SELECT value FROM events`, Arguments: 0}})
		if err != nil {
			return err
		}
		if _, err := otherReader.Resolve(ref); !errors.Is(err, ErrReference) {
			t.Fatalf("foreign reference accepted: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Resolve(saved); !errors.Is(err, ErrReference) {
		t.Fatalf("expired reference accepted: %v", err)
	}
	if _, err := reader.Resolve(storage.ObservationRef{}); !errors.Is(err, ErrReference) {
		t.Fatalf("zero reference accepted: %v", err)
	}
}

func TestObservationFailureReleasesConnection(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	registry := New(db, time.Now)
	want := errors.New("failed owner proof")
	if err := registry.ReadWatchlist(context.Background(), func(storage.ObservationRef, time.Time) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		_ = registry.ReadValuation(context.Background(), func(storage.ObservationRef, time.Time) error { panic("proof panic") })
	}()
	ctx, cancel := context.WithCancel(context.Background())
	if err := registry.ReadValuation(ctx, func(storage.ObservationRef, time.Time) error { cancel(); return nil }); err == nil {
		t.Error("cancelled observation committed")
	}
	check, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := db.PingContext(check); err != nil {
		t.Fatalf("connection retained after failure: %v", err)
	}
}

func TestReadRegistryRejectsWriteAndDynamicStatements(t *testing.T) {
	registry := New(nil, time.Now)
	for _, statement := range []string{`DELETE FROM events RETURNING value`, `SELECT 1; DELETE FROM events`, `PRAGMA writable_schema=1`, `SELECT load_extension(?)`, `SELECT * FROM owner_price_series_versions`, `SELECT * FROM events, order_events`, `SELECT name FROM sqlite_master WHERE 1=1 OR tbl_name='events'`} {
		if _, err := registry.LedgerReader([]Statement{{SQL: statement}}); err == nil {
			t.Errorf("accepted %q", statement)
		}
	}
}

func TestObservationRowsAndDelayedScanCannotEscape(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "rows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE events(value INTEGER); INSERT INTO events VALUES(7)`); err != nil {
		t.Fatal(err)
	}
	registry := New(db, time.Now)
	reader, err := registry.LedgerReader([]Statement{{SQL: `SELECT value FROM events`}})
	if err != nil {
		t.Fatal(err)
	}
	var rows Rows
	var scan Scanner
	err = registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		handle, err := reader.Resolve(ref)
		if err != nil {
			return err
		}
		rows, err = handle.QueryContext(context.Background(), `SELECT value FROM events`)
		scan = handle.QueryOne(context.Background(), `SELECT value FROM events`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() || !errors.Is(rows.Err(), ErrReference) {
		t.Fatal("rows survived observation")
	}
	var value int
	if err := scan.Scan(&value); !errors.Is(err, ErrReference) {
		t.Fatalf("delayed scan survived observation: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestObservationPreservesOneSnapshotAcrossWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE events(value INTEGER); CREATE TABLE fx_observations(value INTEGER); INSERT INTO events VALUES(1); INSERT INTO fx_observations VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	registry := New(db, time.Now)
	ledger, err := registry.LedgerReader([]Statement{{SQL: `SELECT value FROM events`}})
	if err != nil {
		t.Fatal(err)
	}
	market, err := registry.MarketReader([]Statement{{SQL: `SELECT value FROM fx_observations`}})
	if err != nil {
		t.Fatal(err)
	}
	err = registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		l, err := ledger.Resolve(ref)
		if err != nil {
			return err
		}
		var before, after int
		if err := l.QueryOne(context.Background(), `SELECT value FROM events`).Scan(&before); err != nil {
			return err
		}
		if _, err := writer.Exec(`BEGIN; UPDATE events SET value=2; UPDATE fx_observations SET value=2; COMMIT`); err != nil {
			return err
		}
		m, err := market.Resolve(ref)
		if err != nil {
			return err
		}
		if err := m.QueryOne(context.Background(), `SELECT value FROM fx_observations`).Scan(&after); err != nil {
			return err
		}
		if before != 1 || after != 1 {
			t.Fatalf("mixed snapshots %d/%d", before, after)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var committed int
	if err := db.QueryRow(`SELECT value FROM events`).Scan(&committed); err != nil || committed != 2 {
		t.Fatalf("writer did not commit: %d %v", committed, err)
	}
}

func TestOwnerPriceImportPurposeRollbackAndCommitFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "atomic.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE events(value INTEGER PRIMARY KEY); INSERT INTO events VALUES(1); CREATE TABLE owner_price_series_versions(value INTEGER REFERENCES events(value) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
		t.Fatal(err)
	}
	registry := New(db, time.Now)
	importer, err := registry.OwnerPriceWriter(nil, []Statement{{SQL: `INSERT INTO owner_price_series_versions(value) VALUES(?)`, Arguments: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ReadValuation(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		if _, err := importer.Resolve(ref); !errors.Is(err, ErrReference) {
			t.Fatalf("wrong purpose accepted: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("abort after persist")
	err = registry.ImportOwnerPrice(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		handle, err := importer.Resolve(ref)
		if err != nil {
			return err
		}
		if _, err := handle.Execute(context.Background(), `INSERT INTO owner_price_series_versions(value) VALUES(?)`, 1); err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	err = registry.ImportOwnerPrice(context.Background(), func(ref storage.ObservationRef, _ time.Time) error {
		handle, err := importer.Resolve(ref)
		if err != nil {
			return err
		}
		_, err = handle.Execute(context.Background(), `INSERT INTO owner_price_series_versions(value) VALUES(?)`, 2)
		return err
	})
	if err == nil {
		t.Fatal("deferred FK commit failure was hidden")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM owner_price_series_versions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed import persisted %d rows: %v", count, err)
	}
}
