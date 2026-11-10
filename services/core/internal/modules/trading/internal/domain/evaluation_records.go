package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/exact"
	"strings"
)

const paperEvaluationSchema = "paper-operational-evaluation.v1"
const paperEvaluationPolicy = "paper-operational-safety.v1"
const paperPerformanceSchema = "paper-performance-evaluation.v1"
const paperPerformancePolicy = "paper-performance-account-v1"
const paperStrategyPerformanceSchema = "paper-strategy-window-performance.v1"
const paperStrategyPerformancePolicy = "paper-strategy-window-performance-v1"
const noStrategySelection = "no_strategy"

var canonicalUTCString = CanonicalUTCString
var orderJSONHash = JSONHash

func canonicalDecimal(raw string, nonNegative bool) bool {
	if len(raw) == 0 || len(raw) > 64 {
		return false
	}
	value, err := exact.ParseDecimal(raw)
	if err != nil || (nonNegative && value.Sign() < 0) {
		return false
	}
	canonical, err := exact.FormatDecimal(value)
	return err == nil && canonical == raw
}

type PaperEvaluationEvent struct {
	EvaluationID                 string `json:"evaluation_id"`
	SchemaVersion                string `json:"schema_version"`
	PolicyVersion                string `json:"policy_version"`
	AccountRef                   string `json:"account_ref"`
	StrategyResultSHA256         string `json:"strategy_result_sha256"`
	StrategySelectionEventID     string `json:"strategy_selection_event_id"`
	ExpectedPreviousEvaluationID string `json:"expected_previous_evaluation_id"`
	PaperOrderStateSHA256        string `json:"paper_order_state_sha256"`
	OrderCount                   int    `json:"order_count"`
	TerminalOrderCount           int    `json:"terminal_order_count"`
	ActiveOrderCount             int    `json:"active_order_count"`
	PendingActionCount           int    `json:"pending_action_count"`
	Decision                     string `json:"decision"`
	ReasonCode                   string `json:"reason_code"`
	RecordedAt                   string `json:"recorded_at"`
}
type PaperPerformanceEvent struct {
	PerformanceID                   string `json:"performance_id"`
	SchemaVersion                   string `json:"schema_version"`
	PolicyVersion                   string `json:"policy_version"`
	AccountRef                      string `json:"account_ref"`
	PaperAccountingSessionID        string `json:"paper_accounting_session_id"`
	StrategySelectionEventID        string `json:"strategy_selection_event_id"`
	SelectedStrategyResultRef       string `json:"selected_strategy_result_ref"`
	ExpectedPreviousPerformanceID   string `json:"expected_previous_performance_id"`
	StrategySelectionSequenceCutoff int64  `json:"strategy_selection_sequence_cutoff"`
	OrderEventSequenceCutoff        int64  `json:"order_event_sequence_cutoff"`
	PaperMarketSequenceCutoff       int64  `json:"paper_market_sequence_cutoff"`
	AsOf                            string `json:"as_of"`
	PaperAccountStateSHA256         string `json:"paper_account_state_sha256"`
	MarksSHA256                     string `json:"marks_sha256"`
	MarksJSON                       string `json:"marks_json"`
	MarkCount                       int    `json:"mark_count"`
	Cash                            string `json:"cash"`
	OpenCost                        string `json:"open_cost"`
	MarketValue                     string `json:"market_value"`
	RealizedPnL                     string `json:"realized_pnl"`
	UnrealizedPnL                   string `json:"unrealized_pnl"`
	TotalPnL                        string `json:"total_pnl"`
	Equity                          string `json:"equity"`
	PeakEquity                      string `json:"peak_equity"`
	PeriodReturnState               string `json:"period_return_state"`
	PeriodReturn                    string `json:"period_return"`
	CumulativeReturn                string `json:"cumulative_return"`
	Drawdown                        string `json:"drawdown"`
	MaxDrawdown                     string `json:"max_drawdown"`
	RecordedAt                      string `json:"recorded_at"`
}
type PaperStrategyPerformanceEvent struct {
	StrategyPerformanceID                 string `json:"strategy_performance_id"`
	SchemaVersion                         string `json:"schema_version"`
	PolicyVersion                         string `json:"policy_version"`
	AccountRef                            string `json:"account_ref"`
	PaperAccountingSessionID              string `json:"paper_accounting_session_id"`
	StrategySelectionEventID              string `json:"strategy_selection_event_id"`
	SelectedStrategyResultRef             string `json:"selected_strategy_result_ref"`
	ExpectedPreviousStrategyPerformanceID string `json:"expected_previous_strategy_performance_id"`
	BaselinePerformanceID                 string `json:"baseline_performance_id"`
	LatestPerformanceID                   string `json:"latest_performance_id"`
	BaselineAsOf                          string `json:"baseline_as_of"`
	LatestAsOf                            string `json:"latest_as_of"`
	SampleCount                           int    `json:"sample_count"`
	BaselineEquity                        string `json:"baseline_equity"`
	LatestEquity                          string `json:"latest_equity"`
	PeakEquity                            string `json:"peak_equity"`
	PeriodReturnState                     string `json:"period_return_state"`
	PeriodReturn                          string `json:"period_return"`
	CumulativeReturn                      string `json:"cumulative_return"`
	Drawdown                              string `json:"drawdown"`
	MaxDrawdown                           string `json:"max_drawdown"`
	RecordedAt                            string `json:"recorded_at"`
}
type PerformanceMark struct {
	Symbol              string `json:"symbol"`
	Quantity            string `json:"quantity"`
	ObservationID       string `json:"observation_id"`
	ObservationSequence int64  `json:"observation_sequence"`
	Close               string `json:"close"`
	OpenCost            string `json:"open_cost"`
	MarketValue         string `json:"market_value"`
	UnrealizedPnL       string `json:"unrealized_pnl"`
}
type paperPerformanceMark = PerformanceMark

func PaperEvaluationID(event PaperEvaluationEvent) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		event.PolicyVersion, event.AccountRef, event.StrategyResultSHA256,
		event.StrategySelectionEventID, event.PaperOrderStateSHA256,
	}, "\x00")))
	return "paper_evaluation_" + hex.EncodeToString(hash[:16])
}

var paperEvaluationID = PaperEvaluationID

func SamePaperEvaluationSnapshot(left *PaperEvaluationEvent, right PaperEvaluationEvent) bool {
	if left == nil {
		return false
	}
	copy := *left
	copy.ExpectedPreviousEvaluationID = ""
	copy.RecordedAt = ""
	right.ExpectedPreviousEvaluationID = ""
	right.RecordedAt = ""
	return copy == right
}

var samePaperEvaluationSnapshot = SamePaperEvaluationSnapshot

func ValidatePaperEvaluationEvent(event PaperEvaluationEvent) error {
	if !safeOrderID(event.EvaluationID) || event.SchemaVersion != paperEvaluationSchema || event.PolicyVersion != paperEvaluationPolicy ||
		!orderAlias(event.AccountRef, "account") || !strategySHA256Pattern.MatchString(event.StrategyResultSHA256) ||
		!safeOrderID(event.StrategySelectionEventID) || !safeOrderID(event.ExpectedPreviousEvaluationID) ||
		!strategySHA256Pattern.MatchString(event.PaperOrderStateSHA256) || !canonicalUTCString(event.RecordedAt) ||
		event.OrderCount < 0 || event.TerminalOrderCount < 0 || event.ActiveOrderCount < 0 || event.PendingActionCount < 0 ||
		event.OrderCount != event.TerminalOrderCount+event.ActiveOrderCount+event.PendingActionCount {
		return errors.New("paper evaluation event is invalid")
	}
	if paperEvaluationID(event) != event.EvaluationID {
		return errors.New("paper evaluation identity is invalid")
	}
	expectedDecision, expectedReason := paperOperationalDecision(paperOperationalSnapshot{
		Orders: event.OrderCount, Terminal: event.TerminalOrderCount, Active: event.ActiveOrderCount, Pending: event.PendingActionCount,
	})
	if event.Decision != expectedDecision || event.ReasonCode != expectedReason {
		return errors.New("paper evaluation decision is invalid")
	}
	return nil
}

var validatePaperEvaluationEvent = ValidatePaperEvaluationEvent

func PaperPerformanceID(policyVersion, accountRef, sessionID, asOf string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{policyVersion, accountRef, sessionID, asOf}, "\x00")))
	return "paper_performance_" + hex.EncodeToString(hash[:16])
}

var paperPerformanceID = PaperPerformanceID

func PaperStrategyPerformanceID(event PaperStrategyPerformanceEvent) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID, event.StrategySelectionEventID,
		event.BaselinePerformanceID, event.LatestPerformanceID,
	}, "\x00")))
	return "paper_strategy_performance_" + hex.EncodeToString(hash[:16])
}

var paperStrategyPerformanceID = PaperStrategyPerformanceID

func ValidatePaperPerformanceEventShape(event PaperPerformanceEvent) error {
	if !safeOrderID(event.PerformanceID) || event.PerformanceID != paperPerformanceID(event.PolicyVersion, event.AccountRef, event.PaperAccountingSessionID, event.AsOf) ||
		event.SchemaVersion != paperPerformanceSchema || event.PolicyVersion != paperPerformancePolicy ||
		!orderAlias(event.AccountRef, "account") || !safeOrderID(event.PaperAccountingSessionID) ||
		!safeOrderID(event.StrategySelectionEventID) || !safeOrderID(event.ExpectedPreviousPerformanceID) ||
		event.StrategySelectionSequenceCutoff <= 0 || event.OrderEventSequenceCutoff < 0 || event.PaperMarketSequenceCutoff < 0 ||
		!canonicalPaperTimes(event.AsOf, event.RecordedAt) || !strategySHA256Pattern.MatchString(event.PaperAccountStateSHA256) ||
		!strategySHA256Pattern.MatchString(event.MarksSHA256) || event.MarkCount < 0 {
		return errors.New("paper performance event shape is invalid")
	}
	if event.SelectedStrategyResultRef != noStrategySelection && !strategySHA256Pattern.MatchString(event.SelectedStrategyResultRef) {
		return errors.New("paper performance selected strategy is invalid")
	}
	if !canonicalDecimal(event.Cash, true) || !canonicalDecimal(event.OpenCost, true) || !canonicalDecimal(event.MarketValue, true) ||
		!canonicalDecimal(event.RealizedPnL, false) || !canonicalDecimal(event.UnrealizedPnL, false) ||
		!canonicalDecimal(event.TotalPnL, false) || !canonicalDecimal(event.Equity, true) || !canonicalDecimal(event.PeakEquity, true) ||
		!canonicalDecimal(event.CumulativeReturn, false) || !canonicalDecimal(event.Drawdown, true) || !canonicalDecimal(event.MaxDrawdown, true) {
		return errors.New("paper performance values are invalid")
	}
	if (event.PeriodReturnState == "defined" && !canonicalDecimal(event.PeriodReturn, false)) ||
		(event.PeriodReturnState == "undefined_zero_denominator" && event.PeriodReturn != "") ||
		(event.PeriodReturnState != "defined" && event.PeriodReturnState != "undefined_zero_denominator") {
		return errors.New("paper performance period return is invalid")
	}
	var marks []paperPerformanceMark
	if err := json.Unmarshal([]byte(event.MarksJSON), &marks); err != nil || marks == nil || len(marks) != event.MarkCount {
		return errors.New("paper performance marks JSON is invalid")
	}
	canonical, marksSHA, err := orderJSONHash(marks)
	if err != nil || string(canonical) != event.MarksJSON || marksSHA != event.MarksSHA256 {
		return errors.New("paper performance marks JSON or hash mismatch")
	}
	for _, mark := range marks {
		if len(mark.Symbol) != 6 || !safeOrderID(mark.ObservationID) || mark.ObservationSequence <= 0 ||
			!canonicalDecimal(mark.Quantity, true) || !canonicalDecimal(mark.Close, true) || !canonicalDecimal(mark.OpenCost, true) ||
			!canonicalDecimal(mark.MarketValue, true) || !canonicalDecimal(mark.UnrealizedPnL, false) {
			return errors.New("paper performance mark is invalid")
		}
	}
	return nil
}

func ValidatePaperStrategyPerformanceEventShape(event PaperStrategyPerformanceEvent) error {
	if event.StrategyPerformanceID != paperStrategyPerformanceID(event) || !safeOrderID(event.StrategyPerformanceID) ||
		event.SchemaVersion != paperStrategyPerformanceSchema || event.PolicyVersion != paperStrategyPerformancePolicy ||
		!orderAlias(event.AccountRef, "account") || !safeOrderID(event.PaperAccountingSessionID) ||
		!safeOrderID(event.StrategySelectionEventID) || !strategySHA256Pattern.MatchString(event.SelectedStrategyResultRef) ||
		!safeOrderID(event.ExpectedPreviousStrategyPerformanceID) || !safeOrderID(event.BaselinePerformanceID) ||
		!safeOrderID(event.LatestPerformanceID) || event.SampleCount <= 0 ||
		!canonicalPaperTimes(event.BaselineAsOf, event.LatestAsOf, event.RecordedAt) || event.BaselineAsOf > event.LatestAsOf {
		return errors.New("paper strategy performance event shape is invalid")
	}
	for _, raw := range []string{event.BaselineEquity, event.LatestEquity, event.PeakEquity, event.CumulativeReturn, event.Drawdown, event.MaxDrawdown} {
		if _, err := exact.ParseDecimal(raw); err != nil {
			return errors.New("paper strategy performance decimal is invalid")
		}
	}
	if event.PeriodReturnState == "defined" {
		if _, err := exact.ParseDecimal(event.PeriodReturn); err != nil {
			return errors.New("paper strategy performance period return is invalid")
		}
	} else if event.PeriodReturnState != "undefined_zero_denominator" || event.PeriodReturn != "" {
		return errors.New("paper strategy performance period return state is invalid")
	}
	return nil
}

var paperOperationalDecision = PaperOperationalDecision
