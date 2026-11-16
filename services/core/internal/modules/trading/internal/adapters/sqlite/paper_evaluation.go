package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
)

const (
	noPaperEvaluation           = "no_evaluation"
	paperEvaluationSchema       = "paper-operational-evaluation.v1"
	paperEvaluationPolicy       = "paper-operational-safety.v1"
	paperEvaluationInsufficient = "INSUFFICIENT"
	paperEvaluationPass         = "PASS"
	paperEvaluationDegraded     = "DEGRADED"
)

type PaperEvaluationEvent = contracts.PaperEvaluationEvent

type paperOperationalSnapshot = application.OperationalSnapshot

func (s *Store) EvaluatePaperOperations(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperEvaluationEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper evaluation identity is invalid")
	}
	return s.business().EvaluatePaperOperations(ctx, accountRef, resultSHA256, selectionEventID)
}

func derivePaperOperationalSnapshot(ctx context.Context, q orderQuerier, accountRef, resultSHA256, selectionEventID string) (paperOperationalSnapshot, error) {
	return application.DerivePaperOperationalSnapshot(ctx, &operationalReads{q: q}, accountRef, resultSHA256, selectionEventID)
}

var paperOperationalDecision = application.PaperOperationalDecision

var paperEvaluationID = application.PaperEvaluationID

var samePaperEvaluationSnapshot = application.SamePaperEvaluationSnapshot

func latestPaperEvaluationID(ctx context.Context, q orderQuerier, accountRef, selectionEventID string) (string, error) {
	var evaluationID string
	err := q.QueryRowContext(ctx, `SELECT evaluation_id FROM paper_evaluation_events WHERE account_ref=? AND strategy_selection_event_id=? ORDER BY sequence DESC LIMIT 1`, accountRef, selectionEventID).Scan(&evaluationID)
	if errors.Is(err, sql.ErrNoRows) {
		return noPaperEvaluation, nil
	}
	return evaluationID, err
}

func latestPaperEvaluationSequence(ctx context.Context, q orderQuerier) (int64, error) {
	var sequence int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM paper_evaluation_events`).Scan(&sequence)
	return sequence, err
}

func insertPaperEvaluationEvent(ctx context.Context, tx *transaction, event PaperEvaluationEvent) error {
	if err := validatePaperEvaluationEvent(event); err != nil {
		return err
	}
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_evaluation_events(
		evaluation_id,schema_version,policy_version,account_ref,strategy_result_sha256,strategy_selection_event_id,
		expected_previous_evaluation_id,paper_order_state_sha256,order_count,terminal_order_count,active_order_count,
		pending_action_count,decision,reason_code,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.EvaluationID, event.SchemaVersion, event.PolicyVersion,
		event.AccountRef, event.StrategyResultSHA256, event.StrategySelectionEventID, event.ExpectedPreviousEvaluationID,
		event.PaperOrderStateSHA256, event.OrderCount, event.TerminalOrderCount, event.ActiveOrderCount,
		event.PendingActionCount, event.Decision, event.ReasonCode, recordSHA, string(recordJSON), event.RecordedAt)
	return err
}

func loadPaperEvaluationByID(ctx context.Context, q orderQuerier, evaluationID string) (*PaperEvaluationEvent, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT schema_version,policy_version,account_ref,strategy_result_sha256,strategy_selection_event_id,
		expected_previous_evaluation_id,paper_order_state_sha256,order_count,terminal_order_count,active_order_count,
		pending_action_count,decision,reason_code,record_sha256,record_json,recorded_at
		FROM paper_evaluation_events WHERE evaluation_id=?`, evaluationID)
	var event PaperEvaluationEvent
	var recordSHA, recordJSON string
	event.EvaluationID = evaluationID
	err := row.Scan(&event.SchemaVersion, &event.PolicyVersion, &event.AccountRef, &event.StrategyResultSHA256,
		&event.StrategySelectionEventID, &event.ExpectedPreviousEvaluationID, &event.PaperOrderStateSHA256,
		&event.OrderCount, &event.TerminalOrderCount, &event.ActiveOrderCount, &event.PendingActionCount,
		&event.Decision, &event.ReasonCode, &recordSHA, &recordJSON, &event.RecordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateStoredPaperEvaluation(event, recordSHA, recordJSON); err != nil {
		return nil, false, err
	}
	return &event, true, nil
}

func validateStoredPaperEvaluation(event PaperEvaluationEvent, recordSHA, recordJSON string) error {
	if err := validatePaperEvaluationEvent(event); err != nil {
		return err
	}
	canonical, actualSHA, err := orderJSONHash(event)
	if err != nil || string(canonical) != recordJSON || actualSHA != recordSHA {
		return errors.New("paper evaluation metadata or hash mismatch")
	}
	return nil
}

var validatePaperEvaluationEvent = application.ValidatePaperEvaluationEvent
