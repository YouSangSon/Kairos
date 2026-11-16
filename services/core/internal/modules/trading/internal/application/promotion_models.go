package application

import (
	"omni-folio/services/core/internal/modules/trading/contracts"
)

type PromotionClaim struct {
	SchemaVersion             string `json:"schema_version"`
	AttemptID                 string `json:"attempt_id"`
	ExpectedPreviousAttemptID string `json:"expected_previous_attempt_id"`
	AccountRef                string `json:"account_ref"`
	CandidateResultSHA256     string `json:"candidate_result_sha256"`
	IncumbentResultSHA256     string `json:"incumbent_result_sha256"`
	ExpectedCurrentEventID    string `json:"expected_current_event_id"`
	InputDataSHA256           string `json:"input_data_sha256"`
	InputCSV                  string `json:"input_csv"`
	Symbol                    string `json:"symbol"`
	ComparisonStart           string `json:"comparison_start"`
	ComparisonEnd             string `json:"comparison_end"`
	OrderSequenceCutoff       int64  `json:"order_sequence_cutoff"`
	ResearchSequenceCutoff    int64  `json:"research_sequence_cutoff"`
	StartingCash              string `json:"starting_cash"`
	SessionID                 string `json:"session_id"`
	ExecutionPolicySHA256     string `json:"execution_policy_sha256"`
	StateSHA256               string `json:"state_sha256"`
	RecordedAt                string `json:"recorded_at"`
}
type PromotionDecision struct {
	AttemptID              string                              `json:"attempt_id"`
	ClaimSHA256            string                              `json:"claim_sha256"`
	CandidateResultSHA256  string                              `json:"candidate_result_sha256"`
	ExpectedCurrentEventID string                              `json:"expected_current_event_id"`
	Decision               string                              `json:"decision"`
	Reason                 string                              `json:"reason"`
	Comparison             *contracts.PaperPromotionComparison `json:"comparison,omitempty"`
	SelectionEventID       string                              `json:"selection_event_id,omitempty"`
	RecordedAt             string                              `json:"recorded_at"`
}
type Evidence struct {
	contracts.StrategyEvidence
	ArtifactJSON    string                  `json:"-"`
	ExecutionPolicy StrategyExecutionPolicy `json:"-"`
}
