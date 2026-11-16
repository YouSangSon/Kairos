package ledger

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"omni-folio/services/core/internal/modules/ledger/contracts"
)

type correctionReader struct {
	Reader
	target              *Transaction
	lookupErr, countErr error
	uses, countReads    int
}

func (r *correctionReader) TransactionBySource(context.Context, string, string) (*Transaction, error) {
	return r.target, r.lookupErr
}
func (r *correctionReader) CashVoidCount(context.Context, string, string) (int, error) {
	r.countReads++
	return r.uses, r.countErr
}

func TestCashVoidJudgmentPreservesLookupAndFailureOrder(t *testing.T) {
	lookupFailure := errors.New("lookup failed")
	countFailure := errors.New("count failed")
	correction := &Transaction{AccountID: "account-main", Type: "CASH_VOID", OccurredAt: "2026-01-02T00:00:00Z", Currency: "USD", Amount: "-10", CorrectsSourceEventID: "deposit"}
	valid := &Transaction{SourceEventID: "deposit", Type: "DEPOSIT", OccurredAt: "2026-01-01T00:00:00Z", Currency: "USD", Amount: "10"}
	invalid := *valid
	invalid.Amount = "11"
	for _, tc := range []struct {
		name        string
		reader      correctionReader
		unused      bool
		wantFailure error
		wantInvalid bool
		wantReads   int
	}{
		{"invalid target precedes failed count", correctionReader{target: &invalid, countErr: countFailure}, true, nil, true, 0},
		{"missing target", correctionReader{lookupErr: contracts.ErrNotFound}, true, nil, true, 0},
		{"lookup failure", correctionReader{lookupErr: lookupFailure}, true, lookupFailure, false, 0},
		{"valid target count failure", correctionReader{target: valid, countErr: countFailure}, true, countFailure, false, 1},
		{"already corrected", correctionReader{target: valid, uses: 1}, true, nil, true, 1},
		{"recovery does not recheck use", correctionReader{target: valid, countErr: countFailure}, false, nil, false, 0},
		{"unused target", correctionReader{target: valid}, true, nil, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, invalid, err := ValidateCashVoid(context.Background(), &tc.reader, correction, tc.unused)
			if !errors.Is(err, tc.wantFailure) || tc.reader.countReads != tc.wantReads {
				t.Fatalf("error=%v, count reads=%d; want %v/%d", err, tc.reader.countReads, tc.wantFailure, tc.wantReads)
			}
			if tc.wantInvalid {
				want := &APIError{Code: "invalid_correction", Message: "cash void does not exactly reverse one eligible existing cash event", Field: "corrects_source_event_id"}
				if got != nil || !reflect.DeepEqual(invalid, want) {
					t.Fatalf("target=%+v invalid=%+v", got, invalid)
				}
			} else if tc.wantFailure == nil {
				want := &CorrectionTarget{SourceEventID: "deposit", Type: "DEPOSIT", Currency: "USD", Amount: "10"}
				if invalid != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("target=%+v invalid=%+v", got, invalid)
				}
			}
		})
	}
}

type previewAdmissionScope struct {
	WriteScope
	stored     StoredPreview
	current    int64
	currentErr error
	reads      int
}

func (s *previewAdmissionScope) Receipt(context.Context, string) (StoredReceipt, error) {
	return StoredReceipt{}, contracts.ErrNotFound
}
func (s *previewAdmissionScope) Preview(context.Context, string) (StoredPreview, error) {
	return s.stored, nil
}
func (s *previewAdmissionScope) CurrentRevision(context.Context) (int64, error) {
	s.reads++
	return s.current, s.currentErr
}

type previewAdmissionRepo struct {
	Repository
	scope *previewAdmissionScope
}

func (s previewAdmissionRepo) Write(_ context.Context, work func(WriteScope) error) error {
	return work(s.scope)
}

func TestPreviewAdmissionPreservesFailurePriority(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, expiry string
		canApply     bool
		revision     int64
		wantCode     string
		wantKind     contracts.ErrorKind
		reads        int
	}{
		{"expiry wins before row and revision", now.Format(time.RFC3339Nano), false, 2, "stale_preview", contracts.Conflict, 0},
		{"bad expiry wins before row and revision", "bad", false, 2, "stale_preview", contracts.Conflict, 0},
		{"row rejection before revision", now.Add(time.Second).Format(time.RFC3339Nano), false, 2, "preview_has_errors", contracts.InvalidInput, 0},
		{"revision rejection before body decode", now.Add(time.Second).Format(time.RFC3339Nano), true, 2, "stale_preview", contracts.Conflict, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := &previewAdmissionScope{stored: StoredPreview{ExpiresAt: tc.expiry, CanApply: tc.canApply, LedgerRevision: 1, JSON: "bad"}, current: tc.revision}
			calls := 0
			usecase := New(previewAdmissionRepo{scope: scope}, func() time.Time { calls++; return now }, nil, nil)
			_, err := usecase.ApplyImport(context.Background(), ApplyRequest{PreviewID: "preview", IdempotencyKey: "key"})
			if err == nil || err.Kind != tc.wantKind || err.Body.Code != tc.wantCode || scope.reads != tc.reads || calls != 1 {
				t.Fatalf("failure=%+v reads=%d clocks=%d", err, scope.reads, calls)
			}
		})
	}
}
