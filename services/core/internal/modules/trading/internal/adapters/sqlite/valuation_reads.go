package sqlite

import (
	"context"
)

const cashCloseAnchorSQL = "SELECT EXISTS(SELECT 1 FROM paper_market_bar_observations\n\t\t\tWHERE sequence<=? AND source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'\n\t\t\t  AND timezone='Asia/Seoul' AND price_adjustment='unspecified' AND close_at=?\n\t\t\t  AND source_available_at<=? AND fetched_at<=? AND recorded_at<=?)"
const markObservationIDsSQL = "SELECT observation_id FROM paper_market_bar_observations\n\t\tWHERE sequence<=? AND close_at=? ORDER BY sequence"

type valuationReads struct{ q orderQuerier }

func (r *valuationReads) CashCloseAnchor(ctx context.Context, cutoff int64, asOf, recordedAt string) (int, error) {
	var n int
	err := r.q.QueryRowContext(ctx, cashCloseAnchorSQL, cutoff, asOf, recordedAt, recordedAt, recordedAt).Scan(&n)
	return n, err
}
func (r *valuationReads) MarkObservationIDs(ctx context.Context, cutoff int64, asOf string) ([]string, error) {
	rows, err := r.q.QueryContext(ctx, markObservationIDsSQL, cutoff, asOf)
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
func (r *valuationReads) LoadMarkBar(ctx context.Context, id string) (*PaperMarketBarObservation, int64, error) {
	return loadPaperMarketBarByID(ctx, r.q, id)
}
