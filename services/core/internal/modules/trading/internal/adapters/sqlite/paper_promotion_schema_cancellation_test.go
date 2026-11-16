package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type cancelPromotionSchemaRead struct {
	orderQuerier
	cancel  context.CancelFunc
	scanErr error
}

func (q *cancelPromotionSchemaRead) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	if query == "SELECT sql FROM sqlite_master WHERE name=? AND type=? AND tbl_name=?" {
		q.cancel()
		return canceledPromotionSchemaRow{q.orderQuerier.QueryRowContext(ctx, query, args...), q}
	}
	return q.orderQuerier.QueryRowContext(ctx, query, args...)
}

type canceledPromotionSchemaRow struct {
	rowScanner
	query *cancelPromotionSchemaRead
}

func (r canceledPromotionSchemaRow) Scan(dest ...any) error {
	err := r.rowScanner.Scan(dest...)
	r.query.scanErr = err
	return err
}

func TestPaperPromotionSchemaReadPreservesCancellation(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &cancelPromotionSchemaRead{orderQuerier: svc.db, cancel: cancel}
	err := requirePaperPromotionJournalSchema(ctx, q)
	if !errors.Is(q.scanErr, context.Canceled) {
		t.Fatalf("actual SQLite schema read error=%v, want context.Canceled", q.scanErr)
	}
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "paper promotion physical journal schema changed") {
		t.Fatalf("schema read discarded cancellation or schema context: %v", err)
	}
}

func TestPaperPromotionSchemaRejectsMissingOrChangedDefinition(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"missing", "DROP TRIGGER paper_promotion_claims_no_update"},
		{"changed", "DROP TRIGGER paper_promotion_claims_no_update; CREATE TRIGGER paper_promotion_claims_no_update BEFORE UPDATE ON paper_promotion_claims BEGIN SELECT RAISE(ABORT, 'changed'); END"},
	} {
		t.Run(change.name, func(t *testing.T) {
			svc, _ := testService(t, nil, nil)
			if err := requirePaperPromotionJournalSchema(context.Background(), svc.db); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.db.Exec(change.sql); err != nil {
				t.Fatal(err)
			}
			err := requirePaperPromotionJournalSchema(context.Background(), svc.db)
			if err == nil || !strings.HasPrefix(err.Error(), "paper promotion physical journal schema changed") {
				t.Fatalf("changed physical schema was not rejected with its existing error: %v", err)
			}
		})
	}
}
