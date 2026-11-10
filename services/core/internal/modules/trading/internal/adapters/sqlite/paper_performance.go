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
)

const (
	paperPerformanceSchema        = "paper-performance-evaluation.v1"
	paperPerformancePolicy        = "paper-performance-account-v1"
	noPaperPerformancePredecessor = "no_performance"
)

type PaperPerformanceEvent = contracts.PaperPerformanceEvent

type paperPerformanceRecoveryProof = application.PerformanceRecoveryProof

type paperPerformanceMark = application.PerformanceMark

func (s *Store) EvaluatePaperPerformance(ctx context.Context, accountRef, asOf string) (*PaperPerformanceEvent, error) {
	return s.evaluatePaperPerformanceTx(ctx, accountRef, asOf, nil)
}

func (s *Store) evaluatePaperPerformanceWithClaim(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperPerformanceEvent, error) {
	return s.business().EvaluatePaperPerformanceWithClaim(ctx, accountRef, asOf, claim)
}

func (s *Store) evaluatePaperPerformanceTx(ctx context.Context, accountRef, asOf string, claim *paperRunnerClaim) (*PaperPerformanceEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("paper performance evaluator is not configured")
	}
	return s.business().EvaluatePaperPerformance(ctx, accountRef, asOf, claim)
}

var paperPerformanceID = application.PaperPerformanceID

func insertPaperPerformanceEvent(ctx context.Context, tx *transaction, event PaperPerformanceEvent) error {
	if err := validatePaperPerformanceEventShape(event); err != nil {
		return err
	}
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_performance_events(
		performance_id,schema_version,policy_version,account_ref,paper_accounting_session_id,
		strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,
		strategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,
		paper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,
		unrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,
		max_drawdown,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.PerformanceID, event.SchemaVersion, event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID,
		event.StrategySelectionEventID, event.SelectedStrategyResultRef, event.ExpectedPreviousPerformanceID,
		event.StrategySelectionSequenceCutoff, event.OrderEventSequenceCutoff, event.PaperMarketSequenceCutoff, event.AsOf,
		event.PaperAccountStateSHA256, event.MarksSHA256, event.MarksJSON, event.MarkCount, event.Cash, event.OpenCost,
		event.MarketValue, event.RealizedPnL, event.UnrealizedPnL, event.TotalPnL, event.Equity, event.PeakEquity,
		event.PeriodReturnState, event.PeriodReturn, event.CumulativeReturn, event.Drawdown, event.MaxDrawdown,
		recordSHA, string(recordJSON), event.RecordedAt)
	return err
}

func provePaperPerformanceRecovery(ctx context.Context, q orderQuerier) (paperPerformanceRecoveryProof, error) {
	if _, err := provePaperAccountingRecovery(ctx, q); err != nil {
		return paperPerformanceRecoveryProof{}, fmt.Errorf("paper performance prerequisite recovery: %w", err)
	}
	rows, err := q.QueryContext(ctx, `SELECT sequence,performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,
		strategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,
		paper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,
		unrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,
		max_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events ORDER BY sequence`)
	if err != nil {
		return paperPerformanceRecoveryProof{}, err
	}
	type storedPerformance struct {
		sequence              int64
		event                 PaperPerformanceEvent
		recordSHA, recordJSON string
	}
	var stored []storedPerformance
	for rows.Next() {
		var item storedPerformance
		if err := scanPaperPerformance(rows.Scan, &item.event, &item.sequence, &item.recordSHA, &item.recordJSON); err != nil {
			rows.Close()
			return paperPerformanceRecoveryProof{}, err
		}
		stored = append(stored, item)
	}
	if err := closeRows(rows); err != nil {
		return paperPerformanceRecoveryProof{}, err
	}

	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	type accountSeries struct {
		sessionID, previousID, previousAsOf string
		equities                            []string
	}
	series := map[string]*accountSeries{}
	markCount := 0
	for index, item := range stored {
		if item.sequence != int64(index+1) {
			return paperPerformanceRecoveryProof{}, fmt.Errorf("paper performance sequence %d is invalid", item.sequence)
		}
		if err := validateStoredPaperPerformanceEvent(item.event, item.recordSHA, item.recordJSON); err != nil {
			return paperPerformanceRecoveryProof{}, fmt.Errorf("paper performance %q metadata or hash mismatch: %w", item.event.PerformanceID, err)
		}
		if err := rejectLegacyPaperPerformanceAccount(ctx, q, item.event.AccountRef); err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		session, found, err := loadPaperAccountingSession(ctx, q, item.event.AccountRef)
		if err != nil || !found {
			return paperPerformanceRecoveryProof{}, errors.Join(errors.New("paper performance session is missing"), err)
		}
		if session.SessionID != item.event.PaperAccountingSessionID {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance session binding is invalid")
		}
		if err := validatePaperPerformanceWindow(*session, item.event.AsOf, item.event.RecordedAt); err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		if err := validatePaperPerformanceSelection(ctx, q, item.event); err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		states, err := replayPaperAccountingStateWithCutoff(ctx, q, &paperAccountingCutoff{
			OrderSequence: item.event.OrderEventSequenceCutoff, AsOf: item.event.AsOf,
		})
		if err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		state, exists := states[item.event.AccountRef]
		if !exists {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance replay account is missing")
		}
		_, accountSHA, err := orderJSONHash(state)
		if err != nil || accountSHA != item.event.PaperAccountStateSHA256 {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance account digest mismatch")
		}
		marks, valuation, err := derivePaperPerformanceMarks(ctx, q, state, session.StartingCash, item.event.AsOf,
			item.event.RecordedAt, item.event.PaperMarketSequenceCutoff)
		if err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		marksJSON, marksSHA, err := orderJSONHash(marks)
		if err != nil || string(marksJSON) != item.event.MarksJSON || marksSHA != item.event.MarksSHA256 || len(marks) != item.event.MarkCount {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance marks mismatch")
		}
		if valuation.Cash != item.event.Cash || valuation.OpenCost != item.event.OpenCost || valuation.MarketValue != item.event.MarketValue ||
			valuation.RealizedPnL != item.event.RealizedPnL || valuation.UnrealizedPnL != item.event.UnrealizedPnL ||
			valuation.TotalPnL != item.event.TotalPnL || valuation.Equity != item.event.Equity {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance valuation mismatch")
		}
		account := series[item.event.AccountRef]
		if account == nil {
			account = &accountSeries{sessionID: session.SessionID, previousID: noPaperPerformancePredecessor}
			series[item.event.AccountRef] = account
		}
		if account.sessionID != session.SessionID || item.event.ExpectedPreviousPerformanceID != account.previousID ||
			(account.previousAsOf != "" && item.event.AsOf <= account.previousAsOf) {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance predecessor or as_of is invalid")
		}
		points, err := paperdomain.CalculatePerformance(session.StartingCash, append(account.equities, valuation.Equity))
		if err != nil || len(points) == 0 {
			return paperPerformanceRecoveryProof{}, errors.Join(errors.New("paper performance series replay failed"), err)
		}
		point := points[len(points)-1]
		if point.PeakEquity != item.event.PeakEquity || point.PeriodReturnState != item.event.PeriodReturnState ||
			point.PeriodReturn != item.event.PeriodReturn || point.CumulativeReturn != item.event.CumulativeReturn ||
			point.Drawdown != item.event.Drawdown || point.MaxDrawdown != item.event.MaxDrawdown {
			return paperPerformanceRecoveryProof{}, errors.New("paper performance ratio series mismatch")
		}
		if err := encoder.Encode([]any{"paper_performance_events", item.sequence, item.event, item.recordSHA, item.recordJSON}); err != nil {
			return paperPerformanceRecoveryProof{}, err
		}
		account.previousID, account.previousAsOf = item.event.PerformanceID, item.event.AsOf
		account.equities = append(account.equities, valuation.Equity)
		markCount += item.event.MarkCount
	}
	return paperPerformanceRecoveryProof{SHA256: hex.EncodeToString(hash.Sum(nil)), Events: len(stored), Marks: markCount}, nil
}

func scanPaperPerformance(scan func(...any) error, event *PaperPerformanceEvent, sequence *int64, recordSHA, recordJSON *string) error {
	return scan(sequence, &event.PerformanceID, &event.SchemaVersion, &event.PolicyVersion, &event.AccountRef,
		&event.PaperAccountingSessionID, &event.StrategySelectionEventID, &event.SelectedStrategyResultRef,
		&event.ExpectedPreviousPerformanceID, &event.StrategySelectionSequenceCutoff, &event.OrderEventSequenceCutoff,
		&event.PaperMarketSequenceCutoff, &event.AsOf, &event.PaperAccountStateSHA256, &event.MarksSHA256,
		&event.MarksJSON, &event.MarkCount, &event.Cash, &event.OpenCost, &event.MarketValue, &event.RealizedPnL,
		&event.UnrealizedPnL, &event.TotalPnL, &event.Equity, &event.PeakEquity, &event.PeriodReturnState,
		&event.PeriodReturn, &event.CumulativeReturn, &event.Drawdown, &event.MaxDrawdown, recordSHA, recordJSON,
		&event.RecordedAt)
}

func loadPaperPerformanceByKey(ctx context.Context, q orderQuerier, accountRef, asOf string) (*PaperPerformanceEvent, bool, error) {
	row := q.QueryRowContext(ctx, `SELECT 0,performance_id,schema_version,policy_version,account_ref,
		paper_accounting_session_id,strategy_selection_event_id,selected_strategy_result_ref,expected_previous_performance_id,
		strategy_selection_sequence_cutoff,order_event_sequence_cutoff,paper_market_sequence_cutoff,as_of,
		paper_account_state_sha256,marks_sha256,marks_json,mark_count,cash,open_cost,market_value,realized_pnl,
		unrealized_pnl,total_pnl,equity,peak_equity,period_return_state,period_return,cumulative_return,drawdown,
		max_drawdown,record_sha256,record_json,recorded_at FROM paper_performance_events
		WHERE policy_version=? AND account_ref=? AND as_of=?`, paperPerformancePolicy, accountRef, asOf)
	var event PaperPerformanceEvent
	var sequence int64
	var recordSHA, recordJSON string
	err := scanPaperPerformance(row.Scan, &event, &sequence, &recordSHA, &recordJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateStoredPaperPerformanceEvent(event, recordSHA, recordJSON); err != nil {
		return nil, false, err
	}
	return &event, true, nil
}

func currentPaperPerformanceSelection(ctx context.Context, q orderQuerier) (int64, string, string, error) {
	var sequence int64
	var eventID, selected string
	err := q.QueryRowContext(ctx, `SELECT sequence,event_id,selected_result_sha256
		FROM strategy_selection_events ORDER BY sequence DESC LIMIT 1`).Scan(&sequence, &eventID, &selected)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", errors.New("paper performance current strategy selection is missing")
	}
	return sequence, eventID, selected, err
}

func validatePaperPerformanceSelection(ctx context.Context, q orderQuerier, event PaperPerformanceEvent) error {
	var eventID, selected string
	err := q.QueryRowContext(ctx, `SELECT event_id,selected_result_sha256 FROM strategy_selection_events WHERE sequence=?`,
		event.StrategySelectionSequenceCutoff).Scan(&eventID, &selected)
	if err != nil || eventID != event.StrategySelectionEventID || selected != event.SelectedStrategyResultRef {
		return errors.Join(errors.New("paper performance strategy selection cutoff mismatch"), err)
	}
	var orderMax, marketMax int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM order_events`).Scan(&orderMax); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM paper_market_bar_observations`).Scan(&marketMax); err != nil {
		return err
	}
	if event.OrderEventSequenceCutoff < 0 || event.OrderEventSequenceCutoff > orderMax ||
		event.PaperMarketSequenceCutoff < 0 || event.PaperMarketSequenceCutoff > marketMax {
		return errors.New("paper performance evidence cutoff is invalid")
	}
	return nil
}

func rejectLegacyPaperPerformanceAccount(ctx context.Context, q orderQuerier, accountRef string) error {
	var legacy int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_idempotency WHERE mode='paper' AND account_ref=?
		AND json_extract(intent_json, '$.signal_schema_version') IN ('paper-signal.v1','paper-signal.v2')`, accountRef).Scan(&legacy); err != nil {
		return err
	}
	if legacy != 0 {
		return errors.New("paper performance cannot include a legacy paper account")
	}
	return nil
}

var validatePaperPerformanceWindow = application.ValidatePaperPerformanceWindow

func priorPaperPerformanceSeries(ctx context.Context, q orderQuerier, accountRef, sessionID, asOf string) (string, []string, error) {
	rows, err := q.QueryContext(ctx, `SELECT performance_id,paper_accounting_session_id,as_of,equity
		FROM paper_performance_events WHERE account_ref=? ORDER BY sequence`, accountRef)
	if err != nil {
		return "", nil, err
	}
	previous := noPaperPerformancePredecessor
	var equities []string
	for rows.Next() {
		var performanceID, storedSession, storedAsOf, equity string
		if err := rows.Scan(&performanceID, &storedSession, &storedAsOf, &equity); err != nil {
			rows.Close()
			return "", nil, err
		}
		if storedSession != sessionID || storedAsOf >= asOf {
			rows.Close()
			return "", nil, errors.New("paper performance backfill or session change is invalid")
		}
		previous = performanceID
		equities = append(equities, equity)
	}
	if err := closeRows(rows); err != nil {
		return "", nil, err
	}
	return previous, equities, nil
}

func validateStoredPaperPerformanceEvent(event PaperPerformanceEvent, recordSHA, recordJSON string) error {
	if err := validatePaperPerformanceEventShape(event); err != nil {
		return err
	}
	canonical, actualSHA, err := orderJSONHash(event)
	if err != nil || actualSHA != recordSHA || string(canonical) != recordJSON {
		return errors.Join(errors.New("paper performance record JSON or hash mismatch"), err)
	}
	return nil
}

func validatePaperPerformanceEventShape(event PaperPerformanceEvent) error {
	return domain.ValidatePaperPerformanceEventShape(domain.PaperPerformanceEvent(event))
}

func derivePaperPerformanceMarks(ctx context.Context, q orderQuerier, state paperAccountState, startingCash, asOf, recordedAt string, marketCutoff int64) ([]paperPerformanceMark, paperdomain.Valuation, error) {
	return application.DerivePaperPerformanceMarks(ctx, &valuationReads{q: q}, state, startingCash, asOf, recordedAt, marketCutoff)
}
