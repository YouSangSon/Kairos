package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/application/fault"
	"omni-folio/services/core/internal/application/ledger"
	"omni-folio/services/core/internal/ledgerdomain"
)

type Querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Ledger struct{ db *sql.DB }
type ledgerReader struct{ q Querier }
type ledgerWriteScope struct {
	*ledgerReader
	tx *sql.Tx
}

func NewLedger(db *sql.DB) *Ledger            { return &Ledger{db} }
func NewLedgerReader(q Querier) ledger.Reader { return &ledgerReader{q} }

func (s *Ledger) Write(ctx context.Context, work func(ledger.WriteScope) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&ledgerWriteScope{&ledgerReader{tx}, tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Ledger) Read(ctx context.Context, work func(ledger.Reader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&ledgerReader{tx}); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *ledgerReader) CurrentRevision(ctx context.Context) (int64, error) {
	var revision int64
	err := r.q.QueryRowContext(ctx, `SELECT revision FROM ledger_meta WHERE singleton=1`).Scan(&revision)
	return revision, err
}
func (r *ledgerReader) Metadata(ctx context.Context) (ledger.Metadata, error) {
	var result ledger.Metadata
	var verified sql.NullString
	err := r.q.QueryRowContext(ctx, `SELECT revision, recorded_at, last_verified_at FROM ledger_meta WHERE singleton=1`).Scan(&result.Revision, &result.RecordedAt, &verified)
	if verified.Valid {
		result.LastVerifiedAt = &verified.String
	}
	return result, err
}
func (r *ledgerReader) ReplayOrigin(ctx context.Context) (ledger.ReplayOrigin, error) {
	var result ledger.ReplayOrigin
	err := r.q.QueryRowContext(ctx, `SELECT policy,(SELECT revision FROM ledger_meta WHERE singleton=1) FROM ledger_replay_state WHERE singleton=1`).Scan(&result.Policy, &result.Revision)
	return result, err
}
func (r *ledgerReader) EventRange(ctx context.Context) (ledger.EventRange, error) {
	var result ledger.EventRange
	err := r.q.QueryRowContext(ctx, `SELECT COALESCE(MIN(sequence),0),COALESCE(MAX(sequence),0),COUNT(*) FROM events`).Scan(&result.First, &result.Last, &result.Count)
	return result, err
}
func (r *ledgerReader) EventRecordedAt(ctx context.Context, revision int64) (string, error) {
	var at string
	err := r.q.QueryRowContext(ctx, `SELECT recorded_at FROM events WHERE sequence=?`, revision).Scan(&at)
	return at, err
}
func (r *ledgerReader) ReplayEvents(ctx context.Context, policy string, revision int64) ([]ledgerdomain.ReplayEvent, error) {
	order := "occurred_at, sequence"
	if policy == ledgerdomain.FIFOCostBasisPolicy {
		order = "rtrim(occurred_at,'Z'), sequence"
	} else if policy != ledgerdomain.LegacyFIFOCostBasisPolicy {
		return nil, errors.New("unsupported ledger replay policy")
	}
	rows, err := r.q.QueryContext(ctx, `SELECT event_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),receipt_id FROM events WHERE sequence<=? ORDER BY `+order, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []ledgerdomain.ReplayEvent{}
	for rows.Next() {
		var event ledgerdomain.ReplayEvent
		if err := rows.Scan(&event.EventID, &event.Type, &event.OccurredAt, &event.InstrumentID, &event.Symbol, &event.Quantity, &event.Price, &event.Fee, &event.Currency, &event.Amount, &event.CounterCurrency, &event.CounterAmount, &event.ReceiptID); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
func (r *ledgerReader) TransactionBySource(ctx context.Context, accountID, sourceEventID string) (*ledgerdomain.Transaction, error) {
	var tx ledgerdomain.Transaction
	err := r.q.QueryRowContext(ctx, `SELECT event_id,source_event_id,account_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),COALESCE(corrects_source_event_id,'') FROM events WHERE account_id=? AND source_event_id=?`, accountID, sourceEventID).Scan(&tx.EventID, &tx.SourceEventID, &tx.AccountID, &tx.Type, &tx.OccurredAt, &tx.InstrumentID, &tx.Symbol, &tx.Quantity, &tx.Price, &tx.Fee, &tx.Currency, &tx.Amount, &tx.CounterCurrency, &tx.CounterAmount, &tx.CorrectsSourceEventID)
	return &tx, notFound(err)
}
func (r *ledgerReader) CashVoidCount(ctx context.Context, accountID, sourceEventID string) (int, error) {
	var count int
	err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE account_id=? AND corrects_source_event_id=?`, accountID, sourceEventID).Scan(&count)
	return count, err
}
func (s *ledgerWriteScope) Receipt(ctx context.Context, key string) (ledger.StoredReceipt, error) {
	var receipt ledger.StoredReceipt
	err := s.tx.QueryRowContext(ctx, `SELECT request_sha256, receipt_json FROM receipts WHERE idempotency_key=?`, key).Scan(&receipt.RequestSHA256, &receipt.JSON)
	return receipt, notFound(err)
}
func (s *ledgerWriteScope) Preview(ctx context.Context, id string) (ledger.StoredPreview, error) {
	var preview ledger.StoredPreview
	var canApply int
	err := s.tx.QueryRowContext(ctx, `SELECT preview_json,file_sha256,ledger_revision,can_apply,expires_at FROM previews WHERE preview_id=?`, id).Scan(&preview.JSON, &preview.FileSHA256, &preview.LedgerRevision, &canApply, &preview.ExpiresAt)
	preview.CanApply = canApply != 0
	return preview, notFound(err)
}
func (s *ledgerWriteScope) RecordPreview(ctx context.Context, p ledger.StoredPreview) error {
	canApply := 0
	if p.CanApply {
		canApply = 1
	}
	_, err := s.tx.ExecContext(ctx, `INSERT INTO previews(preview_id,file_sha256,ledger_revision,can_apply,preview_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`, p.PreviewID, p.FileSHA256, p.LedgerRevision, canApply, p.JSON, p.ExpiresAt, p.CreatedAt)
	return err
}
func (s *ledgerWriteScope) RecordEvent(ctx context.Context, e ledgerdomain.Transaction, receiptID, recordedAt string) error {
	_, err := s.tx.ExecContext(ctx, `INSERT INTO events(event_id,source_event_id,account_id,type,occurred_at,instrument_id,symbol,quantity,price,fee,currency,amount,counter_currency,counter_amount,corrects_source_event_id,receipt_id,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.EventID, e.SourceEventID, e.AccountID, e.Type, e.OccurredAt, nullable(e.InstrumentID), nullable(e.Symbol), nullable(e.Quantity), nullable(e.Price), nullable(e.Fee), e.Currency, e.Amount, nullable(e.CounterCurrency), nullable(e.CounterAmount), nullable(e.CorrectsSourceEventID), receiptID, recordedAt)
	return err
}
func (s *ledgerWriteScope) RecordAppliedRevision(ctx context.Context, revision int64, recordedAt string) error {
	_, err := s.tx.ExecContext(ctx, `UPDATE ledger_meta SET revision=?, recorded_at=?, last_verified_at=? WHERE singleton=1`, revision, recordedAt, recordedAt)
	return err
}
func (s *ledgerWriteScope) RecordReceipt(ctx context.Context, r ledger.StoredReceipt) error {
	_, err := s.tx.ExecContext(ctx, `INSERT INTO receipts(idempotency_key,request_sha256,receipt_id,receipt_json) VALUES(?,?,?,?)`, r.IdempotencyKey, r.RequestSHA256, r.ReceiptID, r.JSON)
	return err
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fault.ErrNotFound
	}
	return err
}
