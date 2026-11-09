package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	noStrategySelection      = "no_strategy"
	noStrategySelectionEvent = "no_event"
	strategyResultSchema     = "strategy-improvement-result.v1"
	strategyEvaluationPolicy = "sma-expanding-walk-forward.v1"
)

var strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type StrategyEvidence = application.Evidence

type strategyExecutionPolicy = application.StrategyExecutionPolicy

type StrategySelectionEvent = contracts.StrategySelectionEvent

type StrategySelectionState = contracts.StrategySelectionState

type strategyRegistryRecoveryProof = application.StrategyRegistryProof

func validateStrategyOrderSelection(ctx context.Context, q orderQuerier, intent OrderIntent) error {
	if intent.StrategyResultSHA256 == "" && intent.StrategySelectionEventID == "" {
		return nil
	}
	state, err := replayStrategyRegistry(ctx, q)
	if err != nil {
		return err
	}
	if state.SelectedResultSHA256 != intent.StrategyResultSHA256 || state.CurrentEventID != intent.StrategySelectionEventID {
		return errors.New("strategy order is not bound to the current paper candidate")
	}
	return nil
}

func sameStrategyRegistryProof(left, right strategyRegistryRecoveryProof) bool {
	return left.SHA256 == right.SHA256 && left.Evidence == right.Evidence && left.Events == right.Events && left.Evaluations == right.Evaluations &&
		left.SelectedResultSHA256 == right.SelectedResultSHA256 && left.CurrentEventID == right.CurrentEventID
}

func readStrategyArtifact(path string) ([]byte, error) {
	return readBoundedRegularFile(path, maxBodyBytes)
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	// Open first without waiting for a FIFO writer, then validate the actual
	// descriptor. A path-only Stat check would race a regular-file replacement.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("strategy evidence path must be a regular file")
	}
	artifact, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(artifact)) > limit {
		return nil, errors.New("input exceeds the byte limit")
	}
	return artifact, nil
}

func (s *Store) registerStrategyEvidence(ctx context.Context, artifact []byte) (*StrategyEvidence, error) {
	evidence, err := decodeStrategyArtifact(artifact)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := provePaperPerformancePolicyRecovery(ctx, tx); err != nil {
		return nil, fmt.Errorf("strategy evidence policy recovery: %w", err)
	}
	var storedArtifact, recordedAt string
	err = tx.QueryRowContext(ctx, `SELECT artifact_json,recorded_at FROM strategy_research_evidence WHERE result_sha256=?`, evidence.ResultSHA256).Scan(&storedArtifact, &recordedAt)
	if err == nil {
		if storedArtifact != evidence.ArtifactJSON {
			return nil, errors.New("strategy result hash is already bound to different evidence")
		}
		evidence.RecordedAt = recordedAt
		return evidence, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	evidence.RecordedAt = s.now().UTC().Format(time.RFC3339Nano)
	if err := requirePaperPromotionAppendTime(ctx, tx, evidence.RecordedAt); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO strategy_research_evidence(
		result_sha256,artifact_sha256,strategy_name,strategy_version,parameter_sha256,target,artifact_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?)`, evidence.ResultSHA256, evidence.ArtifactSHA256, evidence.StrategyName,
		evidence.StrategyVersion, evidence.ParameterSHA256, evidence.Target, evidence.ArtifactJSON, evidence.RecordedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return evidence, nil
}

func (s *Store) SelectPaperCandidate(ctx context.Context, resultSHA256, expectedCurrentEventID string) (*StrategySelectionState, error) {
	if !strategySHA256Pattern.MatchString(resultSHA256) || !safeOrderID(expectedCurrentEventID) {
		return nil, errors.New("strategy selection identifiers are invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := provePaperPerformancePolicyRecovery(ctx, tx); err != nil {
		return nil, fmt.Errorf("strategy selection policy recovery: %w", err)
	}
	now := s.now().UTC()
	if err := rejectLivePaperRunnerLease(ctx, tx, now); err != nil {
		return nil, err
	}
	state, err := replayStrategyRegistry(ctx, tx)
	if err != nil {
		return nil, err
	}
	if state.CurrentEventID != expectedCurrentEventID {
		return nil, errors.New("stale strategy selection")
	}
	var target string
	if err := tx.QueryRowContext(ctx, `SELECT target FROM strategy_research_evidence WHERE result_sha256=?`, resultSHA256).Scan(&target); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("strategy evidence is not registered")
		}
		return nil, err
	}
	if target != "paper_candidate" {
		return nil, errors.New("only paper_candidate evidence can be selected")
	}
	if state.SelectedResultSHA256 == resultSHA256 {
		return nil, errors.New("strategy evidence is already selected")
	}
	event := StrategySelectionEvent{
		EventID: s.id("strategy_selection"), EventType: "SELECT", CandidateResultSHA256: resultSHA256,
		ExpectedCurrentEventID: expectedCurrentEventID, PreviousSelectedResultSHA256: state.SelectedResultSHA256,
		SelectedResultSHA256: resultSHA256, ReasonCode: "manual_selection", RecordedAt: now.Format(time.RFC3339Nano),
	}
	sequence, err := latestPaperEvaluationSequence(ctx, tx)
	if err != nil {
		return nil, err
	}
	event.PaperEvaluationSequence = sequence
	if err := insertStrategySelectionEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &StrategySelectionState{CurrentEventID: event.EventID, SelectedResultSHA256: resultSHA256}, nil
}

func (s *Store) RollbackPaperCandidate(ctx context.Context, expectedCurrentEventID, sourceEventID string) (*StrategySelectionState, error) {
	if !safeOrderID(expectedCurrentEventID) || !safeOrderID(sourceEventID) {
		return nil, errors.New("strategy rollback identifiers are invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := provePaperPerformancePolicyRecovery(ctx, tx); err != nil {
		return nil, fmt.Errorf("strategy rollback policy recovery: %w", err)
	}
	now := s.now().UTC()
	if err := rejectLivePaperRunnerLease(ctx, tx, now); err != nil {
		return nil, err
	}
	state, err := replayStrategyRegistry(ctx, tx)
	if err != nil {
		return nil, err
	}
	if state.CurrentEventID != expectedCurrentEventID || sourceEventID != state.CurrentEventID {
		return nil, errors.New("stale or mismatched strategy rollback source")
	}
	if err := s.haltAllSyntheticExecutionTx(ctx, tx, now); err != nil {
		return nil, err
	}
	returned, err := s.rollbackPaperCandidateTx(ctx, tx, state, expectedCurrentEventID, sourceEventID, "manual_rollback", "", s.id("strategy_selection"), now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return returned, nil
}

func (s *Store) rollbackPaperCandidateTx(ctx context.Context, tx *sql.Tx, state strategyRegistryRecoveryProof, expectedCurrentEventID, sourceEventID, reasonCode, policyEventID, eventID string, now time.Time) (*StrategySelectionState, error) {
	return s.business().RollbackPaperCandidateTx(ctx, &writeScope{store: s, tx: tx}, state, expectedCurrentEventID, sourceEventID, reasonCode, policyEventID, eventID, now)
}

func insertStrategySelectionEvent(ctx context.Context, tx *sql.Tx, event StrategySelectionEvent) error {
	recordJSON, recordSHA, err := orderJSONHash(event)
	if err != nil {
		return err
	}
	var schema int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&schema); err != nil {
		return err
	}
	if schema >= 27 {
		_, err = tx.ExecContext(ctx, `INSERT INTO strategy_selection_events(
			event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,
			previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,paper_promotion_attempt_id,record_sha256,record_json,recorded_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, event.EventType, nullable(event.CandidateResultSHA256), event.ExpectedCurrentEventID, nullable(event.SourceEventID),
			event.PreviousSelectedResultSHA256, event.SelectedResultSHA256, event.ReasonCode, event.PaperEvaluationSequence, nullable(event.PaperPerformancePolicyEventID), nullable(event.PaperPromotionAttemptID), recordSHA, string(recordJSON), event.RecordedAt)
		return err
	}
	hasPolicySchema, err := hasPaperPerformancePolicySchema(ctx, tx)
	if err != nil {
		return err
	}
	if !hasPolicySchema {
		_, err = tx.ExecContext(ctx, `INSERT INTO strategy_selection_events(
			event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,
			previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,record_sha256,record_json,recorded_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, event.EventType, nullable(event.CandidateResultSHA256),
			event.ExpectedCurrentEventID, nullable(event.SourceEventID), event.PreviousSelectedResultSHA256,
			event.SelectedResultSHA256, event.ReasonCode, event.PaperEvaluationSequence, recordSHA, string(recordJSON), event.RecordedAt)
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO strategy_selection_events(
		event_id,event_type,candidate_result_sha256,expected_current_event_id,source_event_id,
		previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,record_sha256,record_json,recorded_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, event.EventType, nullable(event.CandidateResultSHA256),
		event.ExpectedCurrentEventID, nullable(event.SourceEventID), event.PreviousSelectedResultSHA256,
		event.SelectedResultSHA256, event.ReasonCode, event.PaperEvaluationSequence, nullable(event.PaperPerformancePolicyEventID), recordSHA, string(recordJSON), event.RecordedAt)
	return err
}

func proveStrategyRegistryRecovery(ctx context.Context, q orderQuerier) (strategyRegistryRecoveryProof, error) {
	return replayStrategyRegistry(ctx, q)
}

func replayStrategyRegistry(ctx context.Context, q orderQuerier) (strategyRegistryRecoveryProof, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	var schemaVersion int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&schemaVersion); err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	evidenceTargets := map[string]string{}
	rows, err := q.QueryContext(ctx, `SELECT sequence,result_sha256,artifact_sha256,strategy_name,strategy_version,
		parameter_sha256,target,artifact_json,recorded_at FROM strategy_research_evidence ORDER BY sequence`)
	if err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	evidenceCount := 0
	for rows.Next() {
		var sequence int64
		var resultSHA, artifactSHA, name, version, parameterSHA, target, artifactJSON, recordedAt string
		if err := rows.Scan(&sequence, &resultSHA, &artifactSHA, &name, &version, &parameterSHA, &target, &artifactJSON, &recordedAt); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		evidence, err := decodeStrategyArtifact([]byte(artifactJSON))
		if err != nil || evidence.ResultSHA256 != resultSHA || evidence.ArtifactSHA256 != artifactSHA ||
			evidence.StrategyName != name || evidence.StrategyVersion != version || evidence.ParameterSHA256 != parameterSHA ||
			evidence.Target != target || evidence.ArtifactJSON != artifactJSON || !canonicalUTCString(recordedAt) {
			rows.Close()
			return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy evidence %q metadata or hash mismatch", resultSHA)
		}
		if err := encoder.Encode([]any{"strategy_research_evidence", sequence, resultSHA, artifactSHA, name, version, parameterSHA, target, artifactJSON, recordedAt}); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		evidenceTargets[resultSHA] = target
		evidenceCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return strategyRegistryRecoveryProof{}, err
	}
	if err := rows.Close(); err != nil {
		return strategyRegistryRecoveryProof{}, err
	}

	currentEvent, selected := noStrategySelectionEvent, noStrategySelection
	var stack []string
	eventCount := 0
	knownEvents := map[string]bool{}
	selectionResults := map[string]string{}
	selectionRecordedAt := map[string]time.Time{}
	selectionNextAt := map[string]time.Time{}
	selectionOpenSequence := map[string]int64{}
	selectionCloseSequence := map[string]int64{}
	type policyLink struct{ policyID, eventID string }
	var linkedRollbacks []policyLink
	selectionQuery := `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,0,NULL,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`
	if schemaVersion >= 20 {
		selectionQuery = `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`
	} else if schemaVersion >= 14 {
		selectionQuery = `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,NULL,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`
	}
	if schemaVersion >= 27 {
		selectionQuery = strings.Replace(selectionQuery, "paper_performance_policy_event_id,NULL,record_sha256", "paper_performance_policy_event_id,paper_promotion_attempt_id,record_sha256", 1)
	}
	rows, err = q.QueryContext(ctx, selectionQuery)
	if err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	for rows.Next() {
		var sequence, paperEvaluationSequence int64
		var eventID, eventType, expectedCurrent, previousSelected, nextSelected, reason, recordSHA, recordJSON, recordedAt string
		var candidateSHA, sourceEvent, policyEventID, promotionAttemptID sql.NullString
		if err := rows.Scan(&sequence, &eventID, &eventType, &candidateSHA, &expectedCurrent, &sourceEvent,
			&previousSelected, &nextSelected, &reason, &paperEvaluationSequence, &policyEventID, &promotionAttemptID, &recordSHA, &recordJSON, &recordedAt); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		var event StrategySelectionEvent
		if err := json.Unmarshal([]byte(recordJSON), &event); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		canonical, actualSHA, err := orderJSONHash(event)
		if err != nil || string(canonical) != recordJSON || actualSHA != recordSHA || event.EventID != eventID || event.EventType != eventType ||
			event.CandidateResultSHA256 != candidateSHA.String || event.ExpectedCurrentEventID != expectedCurrent ||
			event.SourceEventID != sourceEvent.String || event.PreviousSelectedResultSHA256 != previousSelected ||
			event.SelectedResultSHA256 != nextSelected || event.ReasonCode != reason ||
			event.PaperEvaluationSequence != paperEvaluationSequence || event.PaperPerformancePolicyEventID != policyEventID.String ||
			(event.PaperPerformancePolicyEventID != "") != policyEventID.Valid ||
			event.PaperPromotionAttemptID != promotionAttemptID.String || (event.PaperPromotionAttemptID != "") != promotionAttemptID.Valid || event.RecordedAt != recordedAt ||
			!safeOrderID(eventID) || !canonicalUTCString(recordedAt) || expectedCurrent != currentEvent || previousSelected != selected {
			rows.Close()
			return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy selection event %q metadata or hash mismatch", eventID)
		}
		switch eventType {
		case "SELECT":
			if evidenceTargets[candidateSHA.String] != "paper_candidate" || sourceEvent.Valid || (reason != "manual_selection" && reason != "auto_comparison") || (reason == "auto_comparison") != promotionAttemptID.Valid || policyEventID.Valid || nextSelected != candidateSHA.String || nextSelected == selected {
				rows.Close()
				return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy selection event %q is invalid", eventID)
			}
			stack = append(stack, nextSelected)
		case "ROLLBACK":
			if promotionAttemptID.Valid || candidateSHA.Valid || !sourceEvent.Valid || sourceEvent.String != currentEvent || !knownEvents[sourceEvent.String] ||
				(reason != "manual_rollback" && reason != "automatic_performance_rollback") ||
				(reason == "automatic_performance_rollback") != policyEventID.Valid || len(stack) == 0 {
				rows.Close()
				return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy rollback event %q is invalid", eventID)
			}
			stack = stack[:len(stack)-1]
			expectedSelected := noStrategySelection
			if len(stack) != 0 {
				expectedSelected = stack[len(stack)-1]
			}
			if nextSelected != expectedSelected {
				rows.Close()
				return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy rollback event %q skipped selection history", eventID)
			}
			if policyEventID.Valid {
				linkedRollbacks = append(linkedRollbacks, policyLink{policyID: policyEventID.String, eventID: eventID})
			}
		default:
			rows.Close()
			return strategyRegistryRecoveryProof{}, fmt.Errorf("strategy selection event %q has unsupported type", eventID)
		}
		selectionHashFields := []any{"strategy_selection_events", sequence, eventID, eventType, candidateSHA, expectedCurrent,
			sourceEvent, previousSelected, nextSelected, reason}
		if schemaVersion >= 14 && paperEvaluationSequence != 0 {
			selectionHashFields = append(selectionHashFields, paperEvaluationSequence)
		}
		if policyEventID.Valid {
			selectionHashFields = append(selectionHashFields, policyEventID)
		}
		if promotionAttemptID.Valid {
			selectionHashFields = append(selectionHashFields, promotionAttemptID)
		}
		selectionHashFields = append(selectionHashFields, recordSHA, recordJSON, recordedAt)
		if err := encoder.Encode(selectionHashFields); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		knownEvents[eventID] = true
		selectionResults[eventID] = nextSelected
		selectionOpenSequence[eventID] = paperEvaluationSequence
		selectionRecordedAt[eventID], _ = canonicalUTCTime(recordedAt)
		if currentEvent != noStrategySelectionEvent {
			selectionNextAt[currentEvent] = selectionRecordedAt[eventID]
			selectionCloseSequence[currentEvent] = paperEvaluationSequence
		}
		currentEvent, selected = eventID, nextSelected
		eventCount++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return strategyRegistryRecoveryProof{}, err
	}
	if err := rows.Close(); err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	for _, link := range linkedRollbacks {
		if err := proveLinkedPaperPerformancePolicyMetadata(ctx, q, link.policyID, "rollback", link.eventID, ""); err != nil {
			return strategyRegistryRecoveryProof{}, err
		}
	}

	evaluationCount := 0
	previousEvaluations := map[string]string{}
	var paperEvaluationTable int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='paper_evaluation_events'`).Scan(&paperEvaluationTable); err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	if paperEvaluationTable == 0 && schemaVersion >= 14 {
		return strategyRegistryRecoveryProof{}, errors.New("paper evaluation table is missing")
	}
	if paperEvaluationTable != 0 {
		rows, err = q.QueryContext(ctx, `SELECT sequence,evaluation_id,schema_version,policy_version,account_ref,
		strategy_result_sha256,strategy_selection_event_id,expected_previous_evaluation_id,paper_order_state_sha256,
		order_count,terminal_order_count,active_order_count,pending_action_count,decision,reason_code,record_sha256,record_json,recorded_at
		FROM paper_evaluation_events ORDER BY sequence`)
		if err != nil {
			return strategyRegistryRecoveryProof{}, err
		}
		for rows.Next() {
			var sequence int64
			var event PaperEvaluationEvent
			var recordSHA, recordJSON string
			if err := rows.Scan(&sequence, &event.EvaluationID, &event.SchemaVersion, &event.PolicyVersion, &event.AccountRef,
				&event.StrategyResultSHA256, &event.StrategySelectionEventID, &event.ExpectedPreviousEvaluationID,
				&event.PaperOrderStateSHA256, &event.OrderCount, &event.TerminalOrderCount, &event.ActiveOrderCount,
				&event.PendingActionCount, &event.Decision, &event.ReasonCode, &recordSHA, &recordJSON, &event.RecordedAt); err != nil {
				rows.Close()
				return strategyRegistryRecoveryProof{}, err
			}
			tuple := event.AccountRef + "\x00" + event.StrategySelectionEventID
			expectedPrevious := noPaperEvaluation
			if previousEvaluations[tuple] != "" {
				expectedPrevious = previousEvaluations[tuple]
			}
			evaluationAt, evaluationTimeOK := canonicalUTCTime(event.RecordedAt)
			selectedAt, selectionTimeOK := selectionRecordedAt[event.StrategySelectionEventID]
			nextAt, hasNextSelection := selectionNextAt[event.StrategySelectionEventID]
			openSequence, hasOpenSequence := selectionOpenSequence[event.StrategySelectionEventID]
			closeSequence, hasCloseSequence := selectionCloseSequence[event.StrategySelectionEventID]
			if err := validateStoredPaperEvaluation(event, recordSHA, recordJSON); err != nil ||
				sequence != int64(evaluationCount+1) || !hasOpenSequence || sequence <= openSequence ||
				(hasCloseSequence && sequence > closeSequence) ||
				event.EvaluationID != paperEvaluationID(event) || event.ExpectedPreviousEvaluationID != expectedPrevious ||
				evidenceTargets[event.StrategyResultSHA256] != "paper_candidate" ||
				selectionResults[event.StrategySelectionEventID] != event.StrategyResultSHA256 || !evaluationTimeOK || !selectionTimeOK ||
				evaluationAt.Before(selectedAt) || (hasNextSelection && evaluationAt.After(nextAt)) {
				rows.Close()
				return strategyRegistryRecoveryProof{}, fmt.Errorf("paper evaluation %q metadata or binding mismatch", event.EvaluationID)
			}
			if err := encoder.Encode([]any{"paper_evaluation_events", sequence, event.EvaluationID, event.SchemaVersion,
				event.PolicyVersion, event.AccountRef, event.StrategyResultSHA256, event.StrategySelectionEventID,
				event.ExpectedPreviousEvaluationID, event.PaperOrderStateSHA256, event.OrderCount, event.TerminalOrderCount,
				event.ActiveOrderCount, event.PendingActionCount, event.Decision, event.ReasonCode, recordSHA, recordJSON, event.RecordedAt}); err != nil {
				rows.Close()
				return strategyRegistryRecoveryProof{}, err
			}
			previousEvaluations[tuple] = event.EvaluationID
			evaluationCount++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, err
		}
		if err := rows.Close(); err != nil {
			return strategyRegistryRecoveryProof{}, err
		}
	}
	for _, sequence := range selectionOpenSequence {
		if sequence > int64(evaluationCount) {
			return strategyRegistryRecoveryProof{}, errors.New("strategy selection references a future paper evaluation sequence")
		}
	}
	if _, err := provePaperPromotionRecovery(ctx, q); err != nil {
		return strategyRegistryRecoveryProof{}, err
	}
	return strategyRegistryRecoveryProof{
		SHA256: hex.EncodeToString(hash.Sum(nil)), Evidence: evidenceCount, Events: eventCount, Evaluations: evaluationCount,
		SelectedResultSHA256: selected, CurrentEventID: currentEvent, Stack: stack,
	}, nil
}

func loadCurrentStrategyExecutionPolicy(ctx context.Context, q orderQuerier, resultSHA256, eventID string) (strategyExecutionPolicy, error) {
	state, err := replayStrategyRegistry(ctx, q)
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	if state.SelectedResultSHA256 != resultSHA256 || state.CurrentEventID != eventID {
		return strategyExecutionPolicy{}, errors.New("strategy execution policy is not current")
	}
	var artifactJSON string
	if err := q.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, resultSHA256).Scan(&artifactJSON); err != nil {
		return strategyExecutionPolicy{}, err
	}
	evidence, err := decodeStrategyArtifact([]byte(artifactJSON))
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	if evidence.ResultSHA256 != resultSHA256 {
		return strategyExecutionPolicy{}, errors.New("strategy execution policy result mismatch")
	}
	return evidence.ExecutionPolicy, nil
}

var decodeStrategyArtifact = application.DecodeStrategyArtifact

var decodeStrategyExecutionContract = application.DecodeStrategyExecutionContract

var strategyCanonicalJSON = application.StrategyCanonicalJSON

var ensureJSONEOF = application.EnsureJSONEOF

var exactKeys = application.ExactKeys

var mapField = application.MapField

var stringField = application.StringField
