package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omni-folio/services/core/internal/modules/trading/internal/application"
)

type readScope struct{ tx *transaction }

// SQLite may still take a writer lock despite ReadOnly, depending on the DSN.
func (s *Store) Read(ctx context.Context, work func(application.ReadScope) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&readScope{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InspectRead(ctx context.Context, work func(application.ReadScope) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return work(&readScope{tx: tx})
}

func (r *readScope) Orders() application.OrderProjectionReads    { return orderProjectionReads{r.tx} }
func (r *readScope) Monitor() application.MonitorProjectionReads { return monitorProjectionReads{r.tx} }
func (r *readScope) InputStatus() application.InputStatusReads   { return inputStatusReads{r.tx} }
func (r *readScope) PromotionReport() application.PromotionReportReads {
	return promotionReportReads{r.tx}
}
func (r *readScope) ResearchReport() application.ResearchReportReads {
	return researchReportReads{r.tx}
}

type orderProjectionReads struct{ tx *transaction }
type monitorProjectionReads struct{ tx *transaction }
type inputStatusReads struct{ tx *transaction }
type promotionReportReads struct{ tx *transaction }
type researchReportReads struct{ tx *transaction }

func (r orderProjectionReads) ProveOrders(ctx context.Context) error {
	_, err := proveOrderRecovery(ctx, r.tx)
	return err
}

func (r orderProjectionReads) LatestOrders(ctx context.Context) ([]application.OrderLogEntry, error) {
	rows, err := r.tx.QueryContext(ctx, `SELECT orders.order_id,latest.recorded_at
		FROM order_idempotency orders
		JOIN order_events latest ON latest.sequence=(
			SELECT MAX(sequence) FROM order_events WHERE order_id=orders.order_id
		)
		ORDER BY latest.sequence DESC`)
	if err != nil {
		return nil, err
	}
	var entries []application.OrderLogEntry
	for rows.Next() {
		var item application.OrderLogEntry
		if err := rows.Scan(&item.OrderID, &item.RecordedAt); err != nil {
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
	return entries, nil
}

func (r orderProjectionReads) Order(ctx context.Context, id string) (application.OrderIntent, *application.OrderState, error) {
	intent, err := loadOrderIntentFrom(ctx, r.tx, id)
	if err != nil {
		return intent, nil, err
	}
	state, err := loadOrderStateFrom(ctx, r.tx, id)
	return intent, state, err
}

func (r monitorProjectionReads) ProveRunner(ctx context.Context) error {
	_, err := provePaperRunnerLeaseRecovery(ctx, r.tx)
	return err
}
func (r monitorProjectionReads) Registry(ctx context.Context) (application.StrategyRegistryProof, error) {
	return replayStrategyRegistry(ctx, r.tx)
}
func (r monitorProjectionReads) SessionCount(ctx context.Context) (int, error) {
	var count int
	err := r.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM paper_accounting_sessions`).Scan(&count)
	return count, err
}
func (r monitorProjectionReads) PendingPolicyCount(ctx context.Context) (int, error) {
	var count int
	err := r.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM paper_performance_events p
		WHERE NOT EXISTS (
			SELECT 1 FROM paper_strategy_performance_events w
			JOIN paper_performance_policy_events e ON e.account_ref=w.account_ref
			AND e.strategy_selection_event_id=w.strategy_selection_event_id
			AND e.strategy_performance_id=w.strategy_performance_id
			WHERE w.account_ref=p.account_ref AND w.strategy_selection_event_id=p.strategy_selection_event_id
			AND w.latest_performance_id=p.performance_id
		)`).Scan(&count)
	return count, err
}
func (r monitorProjectionReads) Lease(ctx context.Context) (application.RunnerLeaseRecord, error) {
	row, err := loadPaperRunnerLease(ctx, r.tx)
	return row.record, err
}
func (r monitorProjectionReads) LatestPolicy(ctx context.Context) (*application.PaperPerformancePolicyEvent, *application.PaperStrategyPerformanceEvent, error) {
	var id string
	err := r.tx.QueryRowContext(ctx, `SELECT policy_event_id FROM paper_performance_policy_events ORDER BY sequence DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	policy, err := loadPaperPerformancePolicyByID(ctx, r.tx, id)
	if err != nil {
		return nil, nil, err
	}
	window, _, err := loadPaperStrategyPerformanceByID(ctx, r.tx, policy.StrategyPerformanceID)
	if err != nil {
		return nil, nil, err
	}
	return &policy, &window, nil
}

func (r inputStatusReads) RequireSchema(ctx context.Context) error {
	return requireSchemaContext(ctx, r.tx)
}
func (r inputStatusReads) ProveRunner(ctx context.Context) error {
	_, err := provePaperRunnerLeaseRecovery(ctx, r.tx)
	return err
}
func (r inputStatusReads) Registry(ctx context.Context) (application.StrategyRegistryProof, error) {
	return replayStrategyRegistry(ctx, r.tx)
}
func (r inputStatusReads) SelectionResult(ctx context.Context, selection string) (string, error) {
	var selected string
	err := r.tx.QueryRowContext(ctx, `SELECT selected_result_sha256 FROM strategy_selection_events WHERE event_id=?`, selection).Scan(&selected)
	return selected, err
}
func (r inputStatusReads) ResearchArtifact(ctx context.Context, selected string) (string, error) {
	var artifact string
	err := r.tx.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, selected).Scan(&artifact)
	return artifact, err
}
func (r inputStatusReads) Session(ctx context.Context, account string) (*application.PaperAccountingSession, bool, error) {
	return loadPaperAccountingSession(ctx, r.tx, account)
}
func (r inputStatusReads) Bars(ctx context.Context, first, last application.PaperMarketBarObservation) (map[string][]application.PaperMarketBarObservation, error) {
	bars, _, err := loadPaperSnapshotRange(ctx, r.tx, first, last)
	return bars, err
}
func (r inputStatusReads) Signal(ctx context.Context, account, id string) (*application.PaperSignalEvent, bool, error) {
	return loadPaperSignalEvent(ctx, r.tx, account, id)
}
func (r inputStatusReads) OrderBySignal(ctx context.Context, account, id string) (string, bool, error) {
	return paperOrderBySignalFrom(ctx, r.tx, account, id)
}
func (r inputStatusReads) OrderState(ctx context.Context, id string) (*application.OrderState, error) {
	return loadOrderStateFrom(ctx, r.tx, id)
}

func (r promotionReportReads) RequireSchema(ctx context.Context) error {
	return requireSchemaContext(ctx, r.tx)
}
func (r promotionReportReads) Registry(ctx context.Context) (application.StrategyRegistryProof, error) {
	return replayStrategyRegistry(ctx, r.tx)
}
func (r promotionReportReads) LatestAttemptID(ctx context.Context) (string, bool, error) {
	var id string
	err := r.tx.QueryRowContext(ctx, `SELECT attempt_id FROM paper_promotion_claims ORDER BY sequence DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}
func (r promotionReportReads) Claim(ctx context.Context, id string) (*application.PromotionClaim, *application.PromotionDecision, bool, error) {
	return loadPaperPromotion(ctx, r.tx, id)
}
func (r promotionReportReads) CandidateArtifact(ctx context.Context, sha string) (string, error) {
	var raw string
	err := r.tx.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=? AND target='paper_candidate'`, sha).Scan(&raw)
	return raw, err
}

func (r researchReportReads) Registry(ctx context.Context) (application.StrategyRegistryProof, error) {
	return replayStrategyRegistry(ctx, r.tx)
}
func (r researchReportReads) LatestArtifact(ctx context.Context) (string, string, error) {
	var artifact, recordedAt string
	err := r.tx.QueryRowContext(ctx, `SELECT artifact_json,recorded_at FROM strategy_research_evidence ORDER BY sequence DESC LIMIT 1`).Scan(&artifact, &recordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return artifact, recordedAt, err
}
