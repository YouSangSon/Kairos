package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type runnerLeaseScope struct{ q orderQuerier }

func (j *runnerLeaseScope) LoadLease(ctx context.Context) (application.RunnerLeaseRecord, error) {
	row, err := loadPaperRunnerLease(ctx, j.q)
	return row.record, err
}
func (j *runnerLeaseScope) CurrentPaperPerformanceSelection(ctx context.Context) (int64, string, string, error) {
	return currentPaperPerformanceSelection(ctx, j.q)
}
func (j *writeScope) LoadLease(ctx context.Context) (application.RunnerLeaseRecord, error) {
	row, err := loadPaperRunnerLease(ctx, j.tx)
	return row.record, err
}
func (j *writeScope) CompareAndSwapLease(ctx context.Context, previous, next application.RunnerLeaseRecord) error {
	return updatePaperRunnerLeaseTx(ctx, j.tx, previous, next)
}
func (j *writeScope) RollbackBinding(ctx context.Context, rollback, policy string) (application.RunnerRollbackBinding, error) {
	var f application.RunnerRollbackBinding
	if err := j.tx.QueryRowContext(ctx, `SELECT event_type,source_event_id,previous_selected_result_sha256,reason_code,paper_performance_policy_event_id FROM strategy_selection_events WHERE event_id=?`, rollback).Scan(&f.EventType, &f.SourceEventID, &f.PreviousResult, &f.ReasonCode, &f.PolicyID); err != nil {
		return f, err
	}
	err := j.tx.QueryRowContext(ctx, `SELECT decision,rollback_selection_event_id FROM paper_performance_policy_events WHERE policy_event_id=?`, policy).Scan(&f.Decision, &f.RollbackEventID)
	return f, err
}
