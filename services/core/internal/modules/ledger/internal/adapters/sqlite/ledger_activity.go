package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	cursorcodec "omni-folio/services/core/internal/modules/ledger/internal/adapters/cursor"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
)

type storedLedgerEvent struct {
	sequence                                           int64
	eventID, sourceEventID, accountID, typ, occurredAt string
	instrumentID, symbol, quantity, price, fee         string
	currency, amount, counterCurrency, counterAmount   string
	correctsSourceEventID, receiptID, recordedAt       string
}

type activityScope struct {
	*ledgerReader
	cursorKey [32]byte
}

func (s *Ledger) ActivityRead(ctx context.Context, work func(ledger.ActivityReader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&activityScope{&ledgerReader{newQueryer(tx)}, s.activityCursorKey}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *activityScope) VerifyActivityStorage(ctx context.Context) error {
	if err := requireSchemaContext(ctx, s.q); err != nil {
		return err
	}
	return proveLedgerReplayState(ctx, s.q, false)
}
func (s *activityScope) ActivityProof(ctx context.Context) (ledger.ActivityProof, error) {
	revision, at, err := (&Service{}).proveLedgerEvents(ctx, s.q)
	return ledger.ActivityProof{Revision: revision, RecordedAt: at}, err
}
func (s *activityScope) CursorCoordinates(ctx context.Context, revision, sequence int64) (string, string, error) {
	var recordedAt, occurredAt string
	if err := s.q.QueryRowContext(ctx, `SELECT recorded_at FROM events WHERE sequence=?`, revision).Scan(&recordedAt); err != nil {
		return "", "", err
	}
	err := s.q.QueryRowContext(ctx, `SELECT occurred_at FROM events WHERE sequence=?`, sequence).Scan(&occurredAt)
	return recordedAt, occurredAt, err
}
func (s *activityScope) EncodeActivityCursor(cursor contracts.ActivityCursor) string {
	return cursorcodec.Encode(s.cursorKey, cursor)
}
func (s *activityScope) ActivityEvents(ctx context.Context, revision int64, limit int, cursor *contracts.ActivityCursor) ([]ledger.ActivityEvent, error) {
	var rows Rows
	var err error
	if cursor == nil {
		rows, err = s.q.QueryContext(ctx, `SELECT sequence,type,occurred_at,recorded_at,COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,'') FROM events WHERE sequence<=? ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT ?`, revision, limit)
	} else {
		rows, err = s.q.QueryContext(ctx, `SELECT sequence,type,occurred_at,recorded_at,COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,'') FROM events WHERE sequence<=? AND (rtrim(occurred_at,'Z')<rtrim(?,'Z') OR (occurred_at=? AND sequence<?)) ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT ?`, revision, cursor.OccurredAt, cursor.OccurredAt, cursor.Sequence, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ledger.ActivityEvent, 0, limit)
	for rows.Next() {
		var entry ledger.ActivityEvent
		if err := rows.Scan(&entry.Sequence, &entry.Type, &entry.OccurredAt, &entry.RecordedAt, &entry.Symbol, &entry.Quantity, &entry.Price, &entry.Fee, &entry.Currency, &entry.Amount, &entry.CounterCurrency, &entry.CounterAmount); err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) proveLedgerEvents(ctx context.Context, tx queryer) (int64, string, error) {
	var revisionValue int64
	var ledgerRecordedAt string
	if err := tx.QueryRowContext(ctx, `SELECT revision,recorded_at FROM ledger_meta WHERE singleton=1`).Scan(&revisionValue, &ledgerRecordedAt); err != nil {
		return 0, "", err
	}
	if !codec.CanonicalUTC(ledgerRecordedAt) {
		return 0, "", errors.New("ledger recorded_at is not canonical UTC")
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,event_id,source_event_id,account_id,type,occurred_at,COALESCE(instrument_id,''),COALESCE(symbol,''),COALESCE(quantity,''),COALESCE(price,''),COALESCE(fee,''),currency,amount,COALESCE(counter_currency,''),COALESCE(counter_amount,''),COALESCE(corrects_source_event_id,''),receipt_id,recorded_at FROM events ORDER BY sequence`)
	if err != nil {
		return 0, "", err
	}
	stored := make([]storedLedgerEvent, 0)
	for rows.Next() {
		var event storedLedgerEvent
		if err := rows.Scan(&event.sequence, &event.eventID, &event.sourceEventID, &event.accountID, &event.typ, &event.occurredAt, &event.instrumentID, &event.symbol, &event.quantity, &event.price, &event.fee, &event.currency, &event.amount, &event.counterCurrency, &event.counterAmount, &event.correctsSourceEventID, &event.receiptID, &event.recordedAt); err != nil {
			rows.Close()
			return 0, "", err
		}
		if event.sequence != int64(len(stored)+1) {
			rows.Close()
			return 0, "", errors.New("ledger event sequence is not contiguous")
		}
		stored = append(stored, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, "", err
	}
	if err := rows.Close(); err != nil {
		return 0, "", err
	}
	if int64(len(stored)) != revisionValue || (len(stored) > 0 && stored[len(stored)-1].sequence != revisionValue) {
		return 0, "", errors.New("ledger revision does not match append-only events")
	}
	for i := range stored {
		if err := s.validateStoredLedgerEvent(ctx, tx, &stored[i]); err != nil {
			return 0, "", err
		}
	}
	activityRecordedAt := zeroTime
	if len(stored) > 0 {
		activityRecordedAt = stored[len(stored)-1].recordedAt
	}
	return revisionValue, activityRecordedAt, nil
}

func (s *Service) validateStoredLedgerEvent(ctx context.Context, tx queryer, stored *storedLedgerEvent) error {
	if stored.eventID == "" || stored.receiptID == "" || !codec.CanonicalUTC(stored.recordedAt) {
		return errors.New("stored ledger event metadata is invalid")
	}
	want := &Transaction{
		EventID: stored.eventID, SourceEventID: stored.sourceEventID, AccountID: stored.accountID,
		Type: stored.typ, OccurredAt: stored.occurredAt, InstrumentID: stored.instrumentID, Symbol: stored.symbol,
		Quantity: stored.quantity, Price: stored.price, Fee: stored.fee, Currency: stored.currency, Amount: stored.amount,
		CounterCurrency: stored.counterCurrency, CounterAmount: stored.counterAmount, CorrectsSourceEventID: stored.correctsSourceEventID,
	}
	normalized, fieldErrors := ledgerdomain.Normalize(ledgerdomain.Transaction(*want))
	if len(fieldErrors) != 0 || !ledger.SameTransaction((*Transaction)(normalized), want) {
		return errors.New("stored ledger event is not canonical")
	}
	if stored.typ == "CASH_VOID" {
		if _, validationErr, err := ledger.ValidateCashVoid(ctx, NewLedgerReader(tx), want, false); err != nil {
			return err
		} else if validationErr != nil {
			return errors.New("stored cash void failed validation")
		}
	}
	return nil
}
