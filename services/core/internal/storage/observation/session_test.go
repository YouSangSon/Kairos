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
		if _, err := handle.Query(context.Background(), `DELETE FROM events RETURNING value`); !errors.Is(err, ErrStatement) {
			t.Fatalf("write query accepted: %v", err)
		}
		if _, err := handle.Query(context.Background(), `SELECT value FROM events`, 1); !errors.Is(err, ErrStatement) {
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
	for _, statement := range []string{`DELETE FROM events RETURNING value`, `SELECT 1; DELETE FROM events`, `PRAGMA writable_schema=1`, `SELECT load_extension(?)`, `SELECT * FROM owner_price_series_versions`} {
		if _, err := registry.LedgerReader([]Statement{{SQL: statement}}); err == nil {
			t.Errorf("accepted %q", statement)
		}
	}
}
