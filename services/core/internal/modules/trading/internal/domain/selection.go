package domain

import (
	"errors"
	"fmt"
)

type StrategySelectionEvent struct {
	EventID                       string `json:"event_id"`
	EventType                     string `json:"event_type"`
	CandidateResultSHA256         string `json:"candidate_result_sha256,omitempty"`
	ExpectedCurrentEventID        string `json:"expected_current_event_id"`
	SourceEventID                 string `json:"source_event_id,omitempty"`
	PreviousSelectedResultSHA256  string `json:"previous_selected_result_sha256"`
	SelectedResultSHA256          string `json:"selected_result_sha256"`
	ReasonCode                    string `json:"reason_code"`
	PaperEvaluationSequence       int64  `json:"paper_evaluation_sequence,omitempty"`
	PaperPerformancePolicyEventID string `json:"paper_performance_policy_event_id,omitempty"`
	PaperPromotionAttemptID       string `json:"paper_promotion_attempt_id,omitempty"`
	RecordedAt                    string `json:"recorded_at"`
}
type SelectionFieldPresence struct{ Candidate, Source, Policy, Promotion bool }

func ApplyStrategySelectionTransition(event StrategySelectionEvent, presence SelectionFieldPresence, candidateTarget, currentEvent, selected string, stack []string, knownSource bool) ([]string, error) {
	switch event.EventType {
	case "SELECT":
		if candidateTarget != "paper_candidate" || presence.Source || (event.ReasonCode != "manual_selection" && event.ReasonCode != "auto_comparison") || (event.ReasonCode == "auto_comparison") != presence.Promotion || presence.Policy || event.SelectedResultSHA256 != event.CandidateResultSHA256 || event.SelectedResultSHA256 == selected {
			return nil, fmt.Errorf("strategy selection event %q is invalid", event.EventID)
		}
		stack = append(stack, event.SelectedResultSHA256)
	case "ROLLBACK":
		if presence.Promotion || presence.Candidate || !presence.Source || event.SourceEventID != currentEvent || !knownSource ||
			(event.ReasonCode != "manual_rollback" && event.ReasonCode != "automatic_performance_rollback") ||
			(event.ReasonCode == "automatic_performance_rollback") != presence.Policy || len(stack) == 0 {
			return nil, fmt.Errorf("strategy rollback event %q is invalid", event.EventID)
		}
		stack = stack[:len(stack)-1]
		expectedSelected := noStrategySelection
		if len(stack) != 0 {
			expectedSelected = stack[len(stack)-1]
		}
		if event.SelectedResultSHA256 != expectedSelected {
			return nil, fmt.Errorf("strategy rollback event %q skipped selection history", event.EventID)
		}
	default:
		return nil, fmt.Errorf("strategy selection event %q has unsupported type", event.EventID)
	}
	return stack, nil
}

func ValidateCurrentOrderSelection(intent OrderIntent, selectedResult, currentEvent string) error {
	if selectedResult != intent.StrategyResultSHA256 || currentEvent != intent.StrategySelectionEventID {
		return errors.New("strategy order is not bound to the current paper candidate")
	}
	return nil
}

func ValidateCurrentExecutionPolicy(selected, current, resultSHA256, eventID string) error {
	if selected != resultSHA256 || current != eventID {
		return errors.New("strategy execution policy is not current")
	}
	return nil
}

func ValidateExecutionPolicyResult(actual, resultSHA256 string) error {
	if actual != resultSHA256 {
		return errors.New("strategy execution policy result mismatch")
	}
	return nil
}

func ValidateExpectedSelection(current, expected string) error {
	if current != expected {
		return errors.New("stale strategy selection")
	}
	return nil
}

func ValidatePaperCandidateSelection(found bool, target, selected, result string) error {
	if !found {
		return errors.New("strategy evidence is not registered")
	}
	if target != "paper_candidate" {
		return errors.New("only paper_candidate evidence can be selected")
	}
	if selected == result {
		return errors.New("strategy evidence is already selected")
	}
	return nil
}

func ValidateRollbackSource(current, expected, source string) error {
	if current != expected || source != current {
		return errors.New("stale or mismatched strategy rollback source")
	}
	return nil
}

func RollbackPaperSelection(stack []string, reasonCode, policyEventID, eventID string) (string, error) {

	if len(stack) == 0 || (reasonCode == "manual_rollback") != (policyEventID == "") ||
		(reasonCode != "manual_rollback" && reasonCode != "automatic_performance_rollback") || !safeOrderID(eventID) {
		return "", errors.New("strategy rollback transition is invalid")
	}
	nextStack := append([]string(nil), stack[:len(stack)-1]...)
	selected := noStrategySelection
	if len(nextStack) != 0 {
		selected = nextStack[len(nextStack)-1]
	}
	return selected, nil
}
