package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	"strings"
)

const (
	paperStrategyPerformanceSchema        = "paper-strategy-window-performance.v1"
	paperStrategyPerformancePolicy        = "paper-strategy-window-performance-v1"
	noPaperStrategyPerformancePredecessor = "no_strategy_performance"
)

type PaperStrategyPerformanceEvent = contracts.PaperStrategyPerformanceEvent

type paperStrategyPerformanceRecoveryProof = application.StrategyPerformanceRecoveryProof

type paperStrategyPerformanceWindow = application.StrategyPerformanceWindow

func (s *Store) EvaluatePaperStrategyPerformance(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
) (*PaperStrategyPerformanceEvent, error) {
	return s.evaluatePaperStrategyPerformanceTx(ctx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, nil)
}

func (s *Store) evaluatePaperStrategyPerformanceWithClaim(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
	claim *paperRunnerClaim,
) (*PaperStrategyPerformanceEvent, error) {
	return s.business().EvaluatePaperStrategyPerformanceWithClaim(ctx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, claim)
}

func (s *Store) evaluatePaperStrategyPerformanceTx(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
	claim *paperRunnerClaim,
) (*PaperStrategyPerformanceEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper strategy performance identifiers are invalid")
	}
	return s.business().EvaluatePaperStrategyPerformance(ctx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID, claim)
}

var paperStrategyPerformanceID = application.PaperStrategyPerformanceID

func insertPaperStrategyPerformanceEvent(ctx context.Context, tx *transaction, event PaperStrategyPerformanceEvent) error {
	if err := validatePaperStrategyPerformanceEventShape(event); err != nil {
		return err
	}
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_strategy_performance_events(
		strategy_performance_id,schema_version,policy_version,account_ref,paper_accounting_session_id,
		strategy_selection_event_id,selected_strategy_result_ref,expected_previous_strategy_performance_id,
		baseline_performance_id,latest_performance_id,baseline_as_of,latest_as_of,sample_count,baseline_equity,
		latest_equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,max_drawdown,
		record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.StrategyPerformanceID, event.SchemaVersion,
		event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID, event.StrategySelectionEventID,
		event.SelectedStrategyResultRef, event.ExpectedPreviousStrategyPerformanceID, event.BaselinePerformanceID,
		event.LatestPerformanceID, event.BaselineAsOf, event.LatestAsOf, event.SampleCount, event.BaselineEquity,
		event.LatestEquity, event.PeakEquity, event.PeriodReturnState, event.PeriodReturn, event.CumulativeReturn,
		event.Drawdown, event.MaxDrawdown, recordSHA, string(recordJSON), event.RecordedAt)
	return err
}

func provePaperStrategyPerformanceRecovery(ctx context.Context, q orderQuerier) (paperStrategyPerformanceRecoveryProof, error) {
	if _, err := provePaperPerformanceRecovery(ctx, q); err != nil {
		return paperStrategyPerformanceRecoveryProof{}, fmt.Errorf("paper strategy performance prerequisite recovery: %w", err)
	}
	rows, err := q.QueryContext(ctx, `SELECT sequence,strategy_performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,
		expected_previous_strategy_performance_id,baseline_performance_id,latest_performance_id,baseline_as_of,
		latest_as_of,sample_count,baseline_equity,latest_equity,peak_equity,period_return_state,period_return,
		cumulative_return,drawdown,max_drawdown,record_sha256,record_json,recorded_at
		FROM paper_strategy_performance_events ORDER BY sequence`)
	if err != nil {
		return paperStrategyPerformanceRecoveryProof{}, err
	}
	type stored struct {
		sequence              int64
		event                 PaperStrategyPerformanceEvent
		recordSHA, recordJSON string
	}
	var events []stored
	for rows.Next() {
		var item stored
		if err := scanPaperStrategyPerformance(rows.Scan, &item.event, &item.sequence, &item.recordSHA, &item.recordJSON); err != nil {
			rows.Close()
			return paperStrategyPerformanceRecoveryProof{}, err
		}
		events = append(events, item)
	}
	if err := closeRows(rows); err != nil {
		return paperStrategyPerformanceRecoveryProof{}, err
	}
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	previous := map[string]string{}
	samples := 0
	for index, item := range events {
		if item.sequence != int64(index+1) {
			return paperStrategyPerformanceRecoveryProof{}, errors.New("paper strategy performance sequence is invalid")
		}
		if err := validateStoredPaperStrategyPerformanceEvent(item.event, item.recordSHA, item.recordJSON); err != nil {
			return paperStrategyPerformanceRecoveryProof{}, fmt.Errorf("paper strategy performance %q metadata or hash mismatch: %w", item.event.StrategyPerformanceID, err)
		}
		var latestSequence int64
		if err := q.QueryRowContext(ctx, `SELECT sequence FROM paper_performance_events WHERE performance_id=?`, item.event.LatestPerformanceID).Scan(&latestSequence); err != nil {
			return paperStrategyPerformanceRecoveryProof{}, err
		}
		window, err := loadPaperStrategyPerformanceWindow(ctx, q, item.event.AccountRef, item.event.PaperAccountingSessionID,
			item.event.StrategySelectionEventID, item.event.SelectedStrategyResultRef, item.event.LatestPerformanceID, latestSequence)
		if err != nil {
			return paperStrategyPerformanceRecoveryProof{}, err
		}
		points, err := paperdomain.CalculatePerformance(window.Baseline.Equity, window.Equities)
		if err != nil || len(points) == 0 {
			return paperStrategyPerformanceRecoveryProof{}, errors.Join(errors.New("paper strategy performance recovery calculation failed"), err)
		}
		point := points[len(points)-1]
		key := strings.Join([]string{item.event.AccountRef, item.event.PaperAccountingSessionID, item.event.StrategySelectionEventID}, "\x00")
		expectedPrevious := noPaperStrategyPerformancePredecessor
		if previous[key] != "" {
			expectedPrevious = previous[key]
		}
		if item.event.ExpectedPreviousStrategyPerformanceID != expectedPrevious ||
			item.event.BaselinePerformanceID != window.Baseline.PerformanceID || item.event.LatestPerformanceID != window.Latest.PerformanceID ||
			item.event.BaselineAsOf != window.Baseline.AsOf || item.event.LatestAsOf != window.Latest.AsOf ||
			item.event.SampleCount != len(window.Equities) || item.event.BaselineEquity != window.Baseline.Equity ||
			item.event.LatestEquity != window.Latest.Equity || item.event.PeakEquity != point.PeakEquity ||
			item.event.PeriodReturnState != point.PeriodReturnState || item.event.PeriodReturn != point.PeriodReturn ||
			item.event.CumulativeReturn != point.CumulativeReturn || item.event.Drawdown != point.Drawdown ||
			item.event.MaxDrawdown != point.MaxDrawdown {
			return paperStrategyPerformanceRecoveryProof{}, errors.New("paper strategy performance window mismatch")
		}
		if err := encoder.Encode([]any{"paper_strategy_performance_events", item.sequence, item.event, item.recordSHA, item.recordJSON}); err != nil {
			return paperStrategyPerformanceRecoveryProof{}, err
		}
		previous[key] = item.event.StrategyPerformanceID
		samples += item.event.SampleCount
	}
	return paperStrategyPerformanceRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Events: len(events), Samples: samples}, nil
}

func loadPaperStrategyPerformanceWindow(ctx context.Context, q orderQuerier, accountRef, sessionID, selectionEventID,
	selectedResult, latestPerformanceID string, latestSequence int64,
) (paperStrategyPerformanceWindow, error) {
	return application.LoadStrategyPerformanceWindow(ctx, &strategyWindowReads{q: q}, accountRef, sessionID, selectionEventID, selectedResult, latestPerformanceID, latestSequence)
}

func loadLatestPaperPerformance(ctx context.Context, q orderQuerier, accountRef string) (PaperPerformanceEvent, int64, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT sequence,performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,
		strategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,
		paper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,
		unrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,
		max_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events
		WHERE account_ref=? ORDER BY sequence DESC LIMIT 1`, accountRef)
	var event PaperPerformanceEvent
	var sequence int64
	var recordSHA, recordJSON string
	err := scanPaperPerformance(row.Scan, &event, &sequence, &recordSHA, &recordJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return PaperPerformanceEvent{}, 0, false, nil
	}
	if err != nil {
		return PaperPerformanceEvent{}, 0, false, err
	}
	if err := validateStoredPaperPerformanceEvent(event, recordSHA, recordJSON); err != nil {
		return PaperPerformanceEvent{}, 0, false, err
	}
	return event, sequence, true, nil
}

func loadPaperStrategyPerformanceByKey(ctx context.Context, q orderQuerier, accountRef, selectionEventID,
	latestPerformanceID string,
) (*PaperStrategyPerformanceEvent, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT 0,strategy_performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,
		expected_previous_strategy_performance_id,baseline_performance_id,latest_performance_id,baseline_as_of,
		latest_as_of,sample_count,baseline_equity,latest_equity,peak_equity,period_return_state,period_return,
		cumulative_return,drawdown,max_drawdown,record_sha256,record_json,recorded_at
		FROM paper_strategy_performance_events WHERE policy_version=? AND account_ref=?
		AND strategy_selection_event_id=? AND latest_performance_id=?`, paperStrategyPerformancePolicy, accountRef,
		selectionEventID, latestPerformanceID)
	var event PaperStrategyPerformanceEvent
	var sequence int64
	var recordSHA, recordJSON string
	err := scanPaperStrategyPerformance(row.Scan, &event, &sequence, &recordSHA, &recordJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateStoredPaperStrategyPerformanceEvent(event, recordSHA, recordJSON); err != nil {
		return nil, false, err
	}
	return &event, true, nil
}

func latestPaperStrategyPerformanceID(ctx context.Context, q orderQuerier, accountRef, sessionID, selectionEventID string) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT strategy_performance_id FROM paper_strategy_performance_events
		WHERE account_ref=? AND paper_accounting_session_id=? AND strategy_selection_event_id=?
		ORDER BY sequence DESC LIMIT 1`, accountRef, sessionID, selectionEventID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return noPaperStrategyPerformancePredecessor, nil
	}
	return id, err
}

func scanPaperStrategyPerformance(scan func(...any) error, event *PaperStrategyPerformanceEvent, sequence *int64,
	recordSHA, recordJSON *string,
) error {
	return scan(sequence, &event.StrategyPerformanceID, &event.SchemaVersion, &event.PolicyVersion, &event.AccountRef,
		&event.PaperAccountingSessionID, &event.StrategySelectionEventID, &event.SelectedStrategyResultRef,
		&event.ExpectedPreviousStrategyPerformanceID, &event.BaselinePerformanceID, &event.LatestPerformanceID,
		&event.BaselineAsOf, &event.LatestAsOf, &event.SampleCount, &event.BaselineEquity, &event.LatestEquity,
		&event.PeakEquity, &event.PeriodReturnState, &event.PeriodReturn, &event.CumulativeReturn, &event.Drawdown,
		&event.MaxDrawdown, recordSHA, recordJSON, &event.RecordedAt)
}

func validateStoredPaperStrategyPerformanceEvent(event PaperStrategyPerformanceEvent, recordSHA, recordJSON string) error {
	if err := validatePaperStrategyPerformanceEventShape(event); err != nil {
		return err
	}
	canonical, actualSHA, err := orderJSONHash(event)
	if err != nil || actualSHA != recordSHA || string(canonical) != recordJSON {
		return errors.Join(errors.New("paper strategy performance record JSON or hash mismatch"), err)
	}
	return nil
}

func validatePaperStrategyPerformanceEventShape(event PaperStrategyPerformanceEvent) error {
	return domain.ValidatePaperStrategyPerformanceEventShape(domain.PaperStrategyPerformanceEvent(event))
}
