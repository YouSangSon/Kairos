package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"time"
)

type inspectionScope struct{ *writeScope }

func (s *Store) Inspect(ctx context.Context, work func(application.InspectionScope) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return work(&inspectionScope{&writeScope{store: s, tx: tx}})
}
func (j *inspectionScope) Evaluations() application.EvaluationInspection { return j }
func (j *inspectionScope) Selections() application.SelectionInspection   { return j }
func (j *inspectionScope) Paper() application.PaperInspection            { return j }
func (j *inspectionScope) Runner() application.RunnerLeaseReader         { return j }
func (j *inspectionScope) Orders() application.OrderInspection           { return j }
func (j *writeScope) LatestAvailableClose(ctx context.Context, now time.Time) (string, error) {
	return latestAvailablePaperClose(ctx, j.tx, now)
}
func (j *writeScope) LatestPerformanceClose(ctx context.Context, account string) (string, error) {
	var previous string
	err := j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(as_of),'') FROM paper_performance_events WHERE account_ref=?`, account).Scan(&previous)
	return previous, err
}
func (j *writeScope) ScheduledResult(ctx context.Context, account, asOf string) (*PaperScheduledRunResult, bool, error) {
	return loadScheduledPaperRunResult(ctx, j.tx, account, asOf)
}

func (j *inspectionScope) Authority() application.AuthorityInspection { return j }
func (j *writeScope) LocalResearch(ctx context.Context, sha string) (application.LocalResearchFacts, error) {
	var f application.LocalResearchFacts
	err := j.tx.QueryRowContext(ctx, `SELECT json_extract(artifact_json,'$.manifest.strategy.parameters.slow_window'),json_extract(artifact_json,'$.input_sha256') FROM strategy_research_evidence WHERE result_sha256=?`, sha).Scan(&f.Slow, &f.SHA256)
	return f, err
}
func (j *writeScope) RecordedSelectionTime(ctx context.Context, id string) (string, error) {
	var at string
	err := j.tx.QueryRowContext(ctx, `SELECT recorded_at FROM strategy_selection_events WHERE event_id=?`, id).Scan(&at)
	return at, err
}
func (j *writeScope) PolicyFrontier(ctx context.Context, account, latest string) (string, int, error) {
	var previous string
	var points int
	err := j.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(as_of),?),COUNT(*) FROM paper_performance_events WHERE account_ref=?`, latest, account).Scan(&previous, &points)
	return previous, points, err
}
func (j *writeScope) PolicyCloses(ctx context.Context, previous, latest, selectedAt, now string, includePrevious, includeOld bool) ([]string, error) {
	rows, err := j.tx.QueryContext(ctx, `SELECT DISTINCT close_at FROM paper_market_bar_observations
 WHERE source='paper_fixture' AND venue='KRX' AND currency='KRW' AND interval='1d'
 AND timezone='Asia/Seoul' AND price_adjustment='unspecified' AND (close_at>? OR (close_at=? AND ?)) AND close_at<=?
 AND (close_at>=? OR (close_at=? AND ?)) AND source_available_at<=? AND fetched_at<=? AND recorded_at<=? ORDER BY close_at`, previous, previous, includePrevious, latest, selectedAt, previous, includeOld, now, now, now)
	if err != nil {
		return nil, err
	}
	var closes []string
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			rows.Close()
			return nil, err
		}
		closes = append(closes, at)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return closes, nil
}
func (j *writeScope) PaperOrderIDs(ctx context.Context, account string) ([]string, error) {
	rows, err := j.tx.QueryContext(ctx, `SELECT order_id FROM order_idempotency WHERE account_ref=? AND mode='paper' ORDER BY recorded_at,order_id`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
