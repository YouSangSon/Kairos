package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/adapters/files"
	"omni-folio/services/core/internal/modules/trading/internal/domain"

	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/application"

	"regexp"

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
	if err := domain.ValidateCurrentOrderSelection(domain.OrderIntent(intent), state.SelectedResultSHA256, state.CurrentEventID); err != nil {
		return err
	}
	return nil
}

func sameStrategyRegistryProof(left, right strategyRegistryRecoveryProof) bool {
	return left.SHA256 == right.SHA256 && left.Evidence == right.Evidence && left.Events == right.Events && left.Evaluations == right.Evaluations &&
		left.SelectedResultSHA256 == right.SelectedResultSHA256 && left.CurrentEventID == right.CurrentEventID
}

var readStrategyArtifact = files.ReadStrategyArtifact

var readBoundedRegularFile = files.ReadBoundedRegularFile

func (s *Store) registerStrategyEvidence(ctx context.Context, artifact []byte) (*StrategyEvidence, error) {
	return s.business().RegisterEvidenceRecord(ctx, artifact)
}

func (s *Store) SelectPaperCandidate(ctx context.Context, resultSHA256, expectedCurrentEventID string) (*StrategySelectionState, error) {
	return s.business().SelectPaperCandidate(ctx, resultSHA256, expectedCurrentEventID)
}

func (s *Store) RollbackPaperCandidate(ctx context.Context, expectedCurrentEventID, sourceEventID string) (*StrategySelectionState, error) {
	return s.business().RollbackPaperCandidate(ctx, expectedCurrentEventID, sourceEventID)
}

func (s *Store) rollbackPaperCandidateTx(ctx context.Context, tx *transaction, state strategyRegistryRecoveryProof, expectedCurrentEventID, sourceEventID, reasonCode, policyEventID, eventID string, now time.Time) (*StrategySelectionState, error) {
	return s.business().RollbackPaperCandidateTx(ctx, &writeScope{store: s, tx: tx}, state, expectedCurrentEventID, sourceEventID, reasonCode, policyEventID, eventID, now)
}

func insertStrategySelectionEvent(ctx context.Context, tx *transaction, event StrategySelectionEvent) error {
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
	if schemaVersion >= 27 {
		rows, err = q.QueryContext(ctx, `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,paper_promotion_attempt_id,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`)
	} else if schemaVersion >= 20 {
		rows, err = q.QueryContext(ctx, `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,paper_performance_policy_event_id,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`)
	} else if schemaVersion >= 14 {
		rows, err = q.QueryContext(ctx, `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,paper_evaluation_sequence,NULL,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`)
	} else {
		rows, err = q.QueryContext(ctx, `SELECT sequence,event_id,event_type,candidate_result_sha256,expected_current_event_id,
		 source_event_id,previous_selected_result_sha256,selected_result_sha256,reason_code,0,NULL,NULL,record_sha256,record_json,recorded_at
		FROM strategy_selection_events ORDER BY sequence`)
	}
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
		nextStack, transitionErr := domain.ApplyStrategySelectionTransition(domain.StrategySelectionEvent(event), domain.SelectionFieldPresence{Candidate: candidateSHA.Valid, Source: sourceEvent.Valid, Policy: policyEventID.Valid, Promotion: promotionAttemptID.Valid}, evidenceTargets[candidateSHA.String], currentEvent, selected, stack, knownEvents[sourceEvent.String])
		if transitionErr != nil {
			rows.Close()
			return strategyRegistryRecoveryProof{}, transitionErr
		}
		stack = nextStack
		if eventType == "ROLLBACK" && policyEventID.Valid {
			linkedRollbacks = append(linkedRollbacks, policyLink{policyID: policyEventID.String, eventID: eventID})
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
	if err := domain.ValidateCurrentExecutionPolicy(state.SelectedResultSHA256, state.CurrentEventID, resultSHA256, eventID); err != nil {
		return strategyExecutionPolicy{}, err
	}
	var artifactJSON string
	if err := q.QueryRowContext(ctx, `SELECT artifact_json FROM strategy_research_evidence WHERE result_sha256=?`, resultSHA256).Scan(&artifactJSON); err != nil {
		return strategyExecutionPolicy{}, err
	}
	evidence, err := decodeStrategyArtifact([]byte(artifactJSON))
	if err != nil {
		return strategyExecutionPolicy{}, err
	}
	if err := domain.ValidateExecutionPolicyResult(evidence.ResultSHA256, resultSHA256); err != nil {
		return strategyExecutionPolicy{}, err
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
