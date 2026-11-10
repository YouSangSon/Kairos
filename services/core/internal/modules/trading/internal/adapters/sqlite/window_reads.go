package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type strategyWindowReads struct{ q orderQuerier }

func (r *strategyWindowReads) WindowSelectionTime(ctx context.Context, selection, result string) (string, error) {
	var at string
	err := r.q.QueryRowContext(ctx, `SELECT recorded_at FROM strategy_selection_events WHERE event_id=? AND selected_result_sha256=?`, selection, result).Scan(&at)
	return at, err
}
func (r *strategyWindowReads) VisitWindowSamples(ctx context.Context, accountRef, sessionID, selectionEventID string, latestSequence int64, visit func(application.PaperPerformanceEvent) error) error {
	rows, err := r.q.QueryContext(ctx, `SELECT 0,performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,
		strategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,
		paper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,
		unrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,
		max_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events
		WHERE account_ref=? AND paper_accounting_session_id=? AND strategy_selection_event_id=? AND sequence<=?
		ORDER BY sequence`, accountRef, sessionID, selectionEventID, latestSequence)
	if err != nil {
		return err
	}
	for rows.Next() {
		var event PaperPerformanceEvent
		var sequence int64
		var recordSHA, recordJSON string
		if err := scanPaperPerformance(rows.Scan, &event, &sequence, &recordSHA, &recordJSON); err != nil {
			rows.Close()
			return err
		}
		if err := visit(event); err != nil {
			rows.Close()
			return err
		}
	}
	return closeRows(rows)
}
