package sqlite

import (
	"context"
	"database/sql"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"time"
)

type LocalOrderLog = contracts.LocalOrderLog

type LocalOrderView = contracts.LocalOrderView

func (s *Store) LocalOrderLog(ctx context.Context) (*LocalOrderLog, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// ponytail: replay the small local log on read; add a verified projection only after measured volume requires it.
	if _, err := proveOrderRecovery(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT orders.order_id,latest.recorded_at
		FROM order_idempotency orders
		JOIN order_events latest ON latest.sequence=(
			SELECT MAX(sequence) FROM order_events WHERE order_id=orders.order_id
		)
		ORDER BY latest.sequence DESC`)
	if err != nil {
		return nil, err
	}
	type entry struct{ orderID, recordedAt string }
	var entries []entry
	for rows.Next() {
		var item entry
		if err := rows.Scan(&item.orderID, &item.recordedAt); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	result := &LocalOrderLog{Source: "local_order_log", BrokerFreshness: "unverified", Orders: []LocalOrderView{}}
	for _, entry := range entries {
		if _, err := time.Parse(time.RFC3339Nano, entry.recordedAt); err != nil {
			return nil, err
		}
		intent, err := loadOrderIntentFrom(ctx, tx, entry.orderID)
		if err != nil {
			return nil, err
		}
		state, err := loadOrderStateFrom(ctx, tx, entry.orderID)
		if err != nil {
			return nil, err
		}
		pendingAction := state.PendingAction
		if pendingAction == "" {
			pendingAction = "none"
		}
		result.Orders = append(result.Orders, LocalOrderView{
			Mode: intent.Mode, Symbol: intent.Symbol, Side: intent.Side, OrderType: intent.OrderType,
			Quantity: state.Quantity, LimitPrice: state.LimitPrice, FilledQuantity: state.FilledQuantity,
			Currency: intent.Currency, Status: state.Status, PendingAction: pendingAction, LastRecordedAt: entry.recordedAt,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
