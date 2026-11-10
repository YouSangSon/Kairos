package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"

	"omni-folio/services/core/internal/modules/trading/internal/application"
)

func TestReadScopeTransactionLifecycle(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	ctx := context.Background()
	if _, err := svc.db.Exec(`CREATE TABLE read_scope_lifecycle (value INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop read")
	for _, test := range []struct {
		name        string
		run         func(context.Context, func(application.ReadScope) error) error
		callbackErr error
		wantCount   int
	}{
		{"commit", svc.Read, nil, 1},
		{"inspect rolls back", svc.InspectRead, nil, 1},
		{"failed read rolls back", svc.Read, stop, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tx *transaction
			err := test.run(ctx, func(scope application.ReadScope) error {
				tx = scope.(*readScope).tx
				if _, err := tx.ExecContext(ctx, `INSERT INTO read_scope_lifecycle (value) VALUES (1)`); err != nil {
					return err
				}
				return test.callbackErr
			})
			if !errors.Is(err, test.callbackErr) {
				t.Fatalf("read error = %v, want %v", err, test.callbackErr)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO read_scope_lifecycle (value) VALUES (2)`); !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("scope remained usable after completion: %v", err)
			}
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM read_scope_lifecycle`).Scan(&count); err != nil || count != test.wantCount {
				t.Fatalf("committed rows = %d, want %d; error = %v", count, test.wantCount, err)
			}
		})
	}
}

func TestReadProjectionsCaptureClockAfterTransactionStarts(t *testing.T) {
	svc, path := testService(t, nil, nil)
	if _, err := svc.db.Exec(`CREATE TABLE read_scope_clock (value INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite3", path+"?_busy_timeout=0&_foreign_keys=on&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	now := time.Date(2026, 1, 10, 15, 1, 0, 0, time.UTC)
	var clockCalls int
	var writeErr error
	svc.now = func() time.Time {
		clockCalls++
		_, writeErr = other.Exec(`INSERT INTO read_scope_clock (value) VALUES (1)`)
		return now
	}
	for _, test := range []struct {
		name string
		read func(context.Context) (string, error)
	}{
		{"monitor", func(ctx context.Context) (string, error) {
			v, err := svc.PaperMonitor(ctx)
			if err != nil {
				return "", err
			}
			return v.ObservedAt, nil
		}},
		{"promotion", func(ctx context.Context) (string, error) {
			v, err := svc.PaperPromotionReport(ctx)
			if err != nil {
				return "", err
			}
			return v.ObservedAt, nil
		}},
		{"research", func(ctx context.Context) (string, error) {
			v, err := svc.ResearchReport(ctx)
			if err != nil {
				return "", err
			}
			return v.ObservedAt, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clockCalls, writeErr = 0, nil
			observed, err := test.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var sqliteErr sqlite3.Error
			if clockCalls != 1 || observed != now.Format(time.RFC3339Nano) || !errors.As(writeErr, &sqliteErr) || sqliteErr.Code != sqlite3.ErrBusy {
				t.Fatalf("clock calls=%d observed=%q competing write error=%v", clockCalls, observed, writeErr)
			}
		})
	}
}
