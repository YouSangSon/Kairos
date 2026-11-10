package sqlite

import (
	"context"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
)

func (s *activityScope) InstrumentTrades(ctx context.Context, instrumentID, currency string, revision int64, limit int) ([]ledger.ActivityEvent, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT type,occurred_at,recorded_at,symbol,quantity,price,fee,amount
  FROM events WHERE instrument_id=? AND currency=? AND type IN ('BUY','SELL') AND sequence<=?
  ORDER BY rtrim(occurred_at,'Z') DESC,sequence DESC LIMIT ?`, instrumentID, currency, revision, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ledger.ActivityEvent{}
	for rows.Next() {
		var entry ledger.ActivityEvent
		if err := rows.Scan(&entry.Type, &entry.OccurredAt, &entry.RecordedAt, &entry.Symbol, &entry.Quantity, &entry.Price, &entry.Fee, &entry.Amount); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return result, nil
}
