package ledger

import (
	"context"
	"errors"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
)

const defaultActivityLimit = 50
const instrumentAverageCostPolicy = "remaining_fifo_cost_per_unit_half_even_8_v1"

type ActivityEvent struct {
	Sequence                                         int64
	Type, OccurredAt, RecordedAt                     string
	Symbol, Quantity, Price, Fee                     string
	Currency, Amount, CounterCurrency, CounterAmount string
}
type ActivityProof struct {
	Revision   int64
	RecordedAt string
}
type ActivityReader interface {
	Reader
	VerifyActivityStorage(context.Context) error
	ActivityProof(context.Context) (ActivityProof, error)
	CursorCoordinates(context.Context, int64, int64) (string, string, error)
	ActivityEvents(context.Context, int64, int, *contracts.ActivityCursor) ([]ActivityEvent, error)
	InstrumentTrades(context.Context, string, string, int64, int) ([]ActivityEvent, error)
	EncodeActivityCursor(contracts.ActivityCursor) string
}

func (s *UseCases) Activity(ctx context.Context, limit int, cursor *contracts.ActivityCursor) (*contracts.LedgerActivityPage, error) {
	var result *contracts.LedgerActivityPage
	err := s.store.ActivityRead(ctx, func(reader ActivityReader) error {
		if err := reader.VerifyActivityStorage(ctx); err != nil {
			return err
		}
		if _, err := Snapshot(ctx, reader); err != nil {
			return err
		}
		current, err := reader.ActivityProof(ctx)
		if err != nil {
			return err
		}
		pageRevision, pageRecordedAt := current.Revision, current.RecordedAt
		if cursor != nil {
			if cursor.LedgerRevision > current.Revision {
				return contracts.ErrInvalidActivityCursor
			}
			revisionRecordedAt, sequenceOccurredAt, err := reader.CursorCoordinates(ctx, cursor.LedgerRevision, cursor.Sequence)
			if err != nil || cursor.LedgerRecordedAt != revisionRecordedAt || cursor.OccurredAt != sequenceOccurredAt {
				return contracts.ErrInvalidActivityCursor
			}
			pageRevision, pageRecordedAt = cursor.LedgerRevision, revisionRecordedAt
		}
		items, err := reader.ActivityEvents(ctx, pageRevision, limit+1, cursor)
		if err != nil {
			return err
		}
		result = &contracts.LedgerActivityPage{Source: "local_ledger", BrokerFreshness: "unverified", LedgerRevision: ledgerdomain.Revision(pageRevision), RecordedAt: pageRecordedAt, Events: []contracts.LedgerActivity{}, NextCursor: nil}
		if len(items) > limit {
			last := items[limit-1]
			next := reader.EncodeActivityCursor(contracts.ActivityCursor{Version: 1, LedgerRevision: pageRevision, LedgerRecordedAt: pageRecordedAt, OccurredAt: last.OccurredAt, Sequence: last.Sequence})
			result.NextCursor = &next
			items = items[:limit]
		}
		for _, entry := range items {
			result.Events = append(result.Events, activityView(entry))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (s *UseCases) InstrumentActivity(ctx context.Context, instrumentID, currency string) (*contracts.InstrumentLedger, *contracts.Error) {
	var result *contracts.InstrumentLedger
	err := s.store.ActivityRead(ctx, func(reader ActivityReader) error {
		snapshot, err := Snapshot(ctx, reader)
		if err != nil {
			return err
		}
		proof, err := reader.ActivityProof(ctx)
		if err != nil {
			return err
		}
		if ledgerdomain.Revision(proof.Revision) != snapshot.LedgerRevision {
			return errors.New("instrument ledger snapshot proof mismatch")
		}
		result = &contracts.InstrumentLedger{InstrumentID: instrumentID, Currency: currency, Source: "local_ledger", BrokerFreshness: "unverified", LedgerRevision: snapshot.LedgerRevision, LedgerAsOf: snapshot.AsOf, LedgerRecordedAt: proof.RecordedAt,
			CostBasisPolicy: snapshot.CostBasisPolicy, AverageCostPolicy: instrumentAverageCostPolicy, Quantity: "0", CostBasis: "0", TradeLimit: defaultActivityLimit, Trades: []contracts.LedgerActivity{}}
		for _, holding := range snapshot.Holdings {
			if holding.InstrumentID != instrumentID || holding.Currency != currency {
				continue
			}
			average, err := ledgerdomain.InstrumentAverageCost(holding.Quantity, holding.CostBasis)
			if err != nil {
				return err
			}
			result.Quantity, result.CostBasis, result.AverageCost = holding.Quantity, holding.CostBasis, &average
			result.Symbol = holding.Symbol
		}
		trades, err := reader.InstrumentTrades(ctx, instrumentID, currency, proof.Revision, defaultActivityLimit+1)
		if err != nil {
			return err
		}
		for _, entry := range trades {
			view := contracts.LedgerActivity{Type: entry.Type, OccurredAt: entry.OccurredAt, RecordedAt: entry.RecordedAt, Symbol: &entry.Symbol, Quantity: &entry.Quantity, Price: &entry.Price, Fee: &entry.Fee, Currency: currency, Amount: entry.Amount}
			if result.Symbol == "" {
				result.Symbol = entry.Symbol
			}
			result.Trades = append(result.Trades, view)
		}
		if len(result.Trades) == 0 {
			return &contracts.Error{Kind: contracts.NotFound, Body: contracts.APIError{Code: "instrument_ledger_not_found", Message: "no trades for this instrument and currency"}}
		}
		result.HasMore = len(result.Trades) > defaultActivityLimit
		if result.HasMore {
			result.Trades = result.Trades[:defaultActivityLimit]
		}
		return nil
	})
	if err != nil {
		return nil, contracts.AsError(err)
	}
	return result, nil
}
func activityView(entry ActivityEvent) contracts.LedgerActivity {
	return contracts.LedgerActivity{Type: entry.Type, OccurredAt: entry.OccurredAt, RecordedAt: entry.RecordedAt, Symbol: optionalActivityText(entry.Symbol), Quantity: optionalActivityText(entry.Quantity), Price: optionalActivityText(entry.Price), Fee: optionalActivityText(entry.Fee), Currency: entry.Currency, Amount: entry.Amount,
		CounterCurrency: optionalActivityText(entry.CounterCurrency), CounterAmount: optionalActivityText(entry.CounterAmount), IsCorrection: entry.Type == "CASH_VOID"}
}
func optionalActivityText(raw string) *string {
	if raw == "" {
		return nil
	}
	return &raw
}
