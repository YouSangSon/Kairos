package sqlite

import (
	"context"
)

type operationalReads struct{ q orderQuerier }

func (r *operationalReads) OperationalOrderIDs(ctx context.Context, account string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT order_id FROM order_idempotency WHERE mode='paper' AND account_ref=? ORDER BY order_id`, account)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return ids, nil
}
func (r *operationalReads) LoadOperationalIntent(ctx context.Context, id string) (OrderIntent, error) {
	return loadOrderIntentFrom(ctx, r.q, id)
}
func (r *operationalReads) LoadOperationalState(ctx context.Context, id string) (*OrderState, error) {
	return loadOrderStateFrom(ctx, r.q, id)
}
