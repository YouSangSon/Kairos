package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"omni-folio/services/core/internal/modules/ledger/contracts"

	"omni-folio/services/core/internal/modules/ledger/internal/fault"

	"omni-folio/services/core/internal/exact"
)

const instrumentAverageCostPolicy = "remaining_fifo_cost_per_unit_half_even_8_v1"

func (s *Service) instrumentLedger(ctx context.Context, instrumentID, currency string) (*InstrumentLedger, *appError) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fault.Internal(err)
	}
	defer tx.Rollback()
	snapshot, err := snapshotFrom(ctx, newQueryer(tx))
	if err != nil {
		return nil, fault.Internal(err)
	}
	ledgerRevision, recordedAt, err := s.proveLedgerEvents(ctx, newQueryer(tx))
	if err != nil {
		return nil, fault.Internal(err)
	}
	if revision(ledgerRevision) != snapshot.LedgerRevision {
		return nil, fault.Internal(errors.New("instrument ledger snapshot proof mismatch"))
	}
	result := &InstrumentLedger{
		InstrumentID: instrumentID, Currency: currency, Source: "local_ledger", BrokerFreshness: "unverified",
		LedgerRevision: snapshot.LedgerRevision, LedgerAsOf: snapshot.AsOf, LedgerRecordedAt: recordedAt,
		CostBasisPolicy: snapshot.CostBasisPolicy, AverageCostPolicy: instrumentAverageCostPolicy,
		Quantity: "0", CostBasis: "0", TradeLimit: defaultActivityLimit, Trades: []LedgerActivity{},
	}
	for _, holding := range snapshot.Holdings {
		if holding.InstrumentID != instrumentID || holding.Currency != currency {
			continue
		}
		quantity, quantityErr := parseDecimal(holding.Quantity)
		cost, costErr := parseDecimal(holding.CostBasis)
		if quantityErr != nil || costErr != nil || quantity.Sign() <= 0 || cost.Sign() < 0 {
			return nil, fault.Internal(errors.New("instrument ledger holding is invalid"))
		}
		// Round only the display ratio; the exact remaining FIFO cost stays unchanged.
		average, err := formatDecimal(exact.QuantizeHalfEven(new(big.Rat).Quo(cost, quantity), 8))
		if err != nil {
			return nil, fault.Internal(err)
		}
		result.Quantity, result.CostBasis, result.AverageCost = holding.Quantity, holding.CostBasis, &average
		result.Symbol = holding.Symbol
	}
	rows, err := tx.QueryContext(ctx, `SELECT type,occurred_at,recorded_at,symbol,quantity,price,fee,amount
		FROM events WHERE instrument_id=? AND currency=? AND type IN ('BUY','SELL') AND sequence<=?
		ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT ?`, instrumentID, currency, ledgerRevision, defaultActivityLimit+1)
	if err != nil {
		return nil, fault.Internal(err)
	}
	for rows.Next() {
		entry := LedgerActivity{Currency: currency}
		var symbol, quantity, price, fee string
		if err := rows.Scan(&entry.Type, &entry.OccurredAt, &entry.RecordedAt, &symbol, &quantity, &price, &fee, &entry.Amount); err != nil {
			rows.Close()
			return nil, fault.Internal(err)
		}
		entry.Symbol, entry.Quantity, entry.Price, entry.Fee = &symbol, &quantity, &price, &fee
		if result.Symbol == "" {
			result.Symbol = symbol
		}
		result.Trades = append(result.Trades, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fault.Internal(err)
	}
	if err := rows.Close(); err != nil {
		return nil, fault.Internal(err)
	}
	if len(result.Trades) == 0 {
		return nil, &appError{Kind: contracts.NotFound, Body: APIError{Code: "instrument_ledger_not_found", Message: "no trades for this instrument and currency"}}
	}
	result.HasMore = len(result.Trades) > defaultActivityLimit
	if result.HasMore {
		result.Trades = result.Trades[:defaultActivityLimit]
	}
	if err := tx.Commit(); err != nil {
		return nil, fault.Internal(err)
	}
	return result, nil
}
