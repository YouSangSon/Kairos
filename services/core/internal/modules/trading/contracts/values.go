package contracts

import (
	"omni-folio/services/core/internal/orderdomain"
	"omni-folio/services/core/internal/researchdomain"
)

type ExecutionAuthorityState struct {
	AccountRef     string `json:"account_ref"`
	Armed          bool   `json:"armed"`
	LeaseOwner     string `json:"lease_owner,omitempty"`
	FencingToken   int64  `json:"fencing_token"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
}

type OrderLookup struct {
	Provider   string
	Mode       string
	AccountRef string
	ObservedAt string
	Complete   bool
	Orders     []OrderObservation
}

type OrderObservation struct {
	ProviderOrderRef   string
	Symbol             string
	Exchange           string
	Side               string
	OrderType          string
	Currency           string
	Quantity           string
	LimitPrice         string
	RemainingQuantity  string
	SubmittedAt        string
	ExecutionsComplete bool
	Executions         []ExecutionObservation
}

type ExecutionObservation struct {
	ProviderExecutionRef string
	Quantity             string
	Price                string
	OccurredAt           string
}

type OrderReconciliation struct {
	Outcome string
	State   *OrderState
}

type LocalOrderLog struct {
	Source          string           `json:"source"`
	BrokerFreshness string           `json:"broker_freshness"`
	Orders          []LocalOrderView `json:"orders"`
}

type LocalOrderView struct {
	Mode           string `json:"mode"`
	Symbol         string `json:"symbol"`
	Side           string `json:"side"`
	OrderType      string `json:"order_type"`
	Quantity       string `json:"quantity"`
	LimitPrice     string `json:"limit_price"`
	FilledQuantity string `json:"filled_quantity"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	PendingAction  string `json:"pending_action"`
	LastRecordedAt string `json:"last_recorded_at"`
}

type OrderIntent struct {
	ClientOrderID                string `json:"client_order_id"`
	Provider                     string `json:"provider"`
	Mode                         string `json:"mode"`
	AccountRef                   string `json:"account_ref"`
	Symbol                       string `json:"symbol"`
	Exchange                     string `json:"exchange"`
	Side                         string `json:"side"`
	OrderType                    string `json:"order_type"`
	Quantity                     string `json:"quantity"`
	LimitPrice                   string `json:"limit_price"`
	Currency                     string `json:"currency"`
	StrategyResultSHA256         string `json:"strategy_result_sha256,omitempty"`
	StrategySelectionEventID     string `json:"strategy_selection_event_id,omitempty"`
	SignalSchemaVersion          string `json:"signal_schema_version,omitempty"`
	SignalID                     string `json:"signal_id,omitempty"`
	SignalDataSHA256             string `json:"signal_data_sha256,omitempty"`
	SignalDataAsOf               string `json:"signal_data_as_of,omitempty"`
	SignalGeneratedAt            string `json:"signal_generated_at,omitempty"`
	SignalExpiresAt              string `json:"signal_expires_at,omitempty"`
	SignalTargetQuantity         string `json:"signal_target_quantity,omitempty"`
	PaperAccountingSessionID     string `json:"paper_accounting_session_id,omitempty"`
	PaperAccountingPolicyVersion string `json:"paper_accounting_policy_version,omitempty"`
	PaperSignalEventID           string `json:"paper_signal_event_id,omitempty"`
	ExecutionPolicySHA256        string `json:"execution_policy_sha256,omitempty"`
}

type OrderEvent = orderdomain.Event

type OrderState orderdomain.State

type PaperAccountingSession struct {
	SessionID                string `json:"session_id"`
	SchemaVersion            string `json:"schema_version"`
	AccountRef               string `json:"account_ref"`
	StrategyResultSHA256     string `json:"strategy_result_sha256"`
	StrategySelectionEventID string `json:"strategy_selection_event_id"`
	ExecutionPolicySHA256    string `json:"execution_policy_sha256"`
	ExecutionPolicyJSON      string `json:"execution_policy_json"`
	StartingCash             string `json:"starting_cash"`
	Currency                 string `json:"currency"`
	RecordedAt               string `json:"recorded_at"`
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

type PaperImprovementReceipt struct {
	SchemaVersion   string                 `json:"schema_version"`
	ResultSHA256    string                 `json:"result_sha256"`
	Registered      bool                   `json:"registered"`
	AttemptRecorded bool                   `json:"attempt_recorded"`
	Disposition     string                 `json:"disposition"`
	Comparison      *PaperPromotionReceipt `json:"comparison"`
}

type PaperInputStatus struct {
	Mode                    string  `json:"mode"`
	ObservedAt              string  `json:"observed_at"`
	ProposalSHA256          string  `json:"proposal_sha256"`
	InputSHA256             string  `json:"input_sha256"`
	DataAsOf                string  `json:"data_as_of"`
	Signal                  string  `json:"signal"`
	CurrentSelectionMatches bool    `json:"current_selection_matches"`
	BarState                string  `json:"bar_state"`
	BarRecordedAt           *string `json:"bar_recorded_at"`
	ProposalDeadline        *string `json:"proposal_deadline"`
	SignalRecorded          bool    `json:"signal_recorded"`
	OrderStatus             *string `json:"order_status"`
	Completion              string  `json:"completion"`
	ExecutionAuthorized     bool    `json:"execution_authorized"`
	Freshness               string  `json:"freshness"`
}

type LocalPaperStepResult struct {
	Mode     string                   `json:"mode"`
	Snapshot *PaperSnapshotImport     `json:"snapshot"`
	Order    *OrderState              `json:"order,omitempty"`
	Policy   *PaperScheduledRunResult `json:"policy"`
}

type PaperMarketBarObservation struct {
	ObservationID       string `json:"observation_id"`
	SchemaVersion       string `json:"schema_version"`
	Source              string `json:"source"`
	SourceObservationID string `json:"source_observation_id"`
	InputDataSHA256     string `json:"input_data_sha256"`
	Symbol              string `json:"symbol"`
	Venue               string `json:"venue"`
	Currency            string `json:"currency"`
	Interval            string `json:"interval"`
	Timezone            string `json:"timezone"`
	PriceAdjustment     string `json:"price_adjustment"`
	Open                string `json:"open"`
	High                string `json:"high"`
	Low                 string `json:"low"`
	Close               string `json:"close"`
	Volume              string `json:"volume"`
	OpenAt              string `json:"open_at"`
	CloseAt             string `json:"close_at"`
	SourceAvailableAt   string `json:"source_available_at"`
	FetchedAt           string `json:"fetched_at"`
	RecordedAt          string `json:"recorded_at"`
}

type PaperSignalEvent struct {
	EventID                         string `json:"event_id"`
	SchemaVersion                   string `json:"schema_version"`
	AccountRef                      string `json:"account_ref"`
	PaperAccountingSessionID        string `json:"paper_accounting_session_id"`
	StrategyResultSHA256            string `json:"strategy_result_sha256"`
	StrategySelectionEventID        string `json:"strategy_selection_event_id"`
	ExecutionPolicySHA256           string `json:"execution_policy_sha256"`
	SignalID                        string `json:"signal_id"`
	SignalBarObservationID          string `json:"signal_bar_observation_id"`
	DataSHA256                      string `json:"data_sha256"`
	Symbol                          string `json:"symbol"`
	TargetQuantity                  string `json:"target_quantity"`
	DataAsOf                        string `json:"data_as_of"`
	GeneratedAt                     string `json:"generated_at"`
	ExpiresAt                       string `json:"expires_at"`
	MarketObservationSequenceCutoff int64  `json:"market_observation_sequence_cutoff"`
	RecordedAt                      string `json:"recorded_at"`
}

type PaperMonitor struct {
	SchemaVersion      string              `json:"schema_version"`
	Mode               string              `json:"mode"`
	ObservedAt         string              `json:"observed_at"`
	StrategySelected   bool                `json:"strategy_selected"`
	SessionCount       int                 `json:"session_count"`
	PendingPolicyCount int                 `json:"pending_policy_count"`
	Runner             PaperMonitorRunner  `json:"runner"`
	LatestPolicy       *PaperMonitorPolicy `json:"latest_policy"`
}

type PaperMonitorRunner struct {
	State       string  `json:"state"`
	HeartbeatAt *string `json:"heartbeat_at"`
	ExpiresAt   *string `json:"expires_at"`
}

type PaperMonitorPolicy struct {
	AsOf                    string `json:"as_of"`
	RecordedAt              string `json:"recorded_at"`
	Decision                string `json:"decision"`
	ReasonCode              string `json:"reason_code"`
	MatchesCurrentSelection bool   `json:"matches_current_selection"`
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

type PaperPerformancePolicyEvent struct {
	PolicyEventID                          string `json:"policy_event_id"`
	SchemaVersion                          string `json:"schema_version"`
	PolicyVersion                          string `json:"policy_version"`
	AccountRef                             string `json:"account_ref"`
	PaperAccountingSessionID               string `json:"paper_accounting_session_id"`
	StrategySelectionEventID               string `json:"strategy_selection_event_id"`
	SelectedStrategyResultRef              string `json:"selected_strategy_result_ref"`
	StrategyPerformanceID                  string `json:"strategy_performance_id"`
	BaselinePerformanceID                  string `json:"baseline_performance_id"`
	LatestPerformanceID                    string `json:"latest_performance_id"`
	SampleCount                            int    `json:"sample_count"`
	ExpectedPreviousPolicyEventID          string `json:"expected_previous_policy_event_id"`
	StrategySelectionSequenceCutoff        int64  `json:"strategy_selection_sequence_cutoff"`
	PaperStrategyPerformanceSequenceCutoff int64  `json:"paper_strategy_performance_sequence_cutoff"`
	ExecutionAuthoritySequenceCutoff       int64  `json:"execution_authority_sequence_cutoff"`
	Decision                               string `json:"decision"`
	ReasonCode                             string `json:"reason_code"`
	RollbackSelectionEventID               string `json:"rollback_selection_event_id,omitempty"`
	AutomaticHaltCount                     int    `json:"automatic_halt_count"`
	RecordedAt                             string `json:"recorded_at"`
}

type PaperPromotionReceipt struct {
	AttemptID        string                    `json:"attempt_id"`
	InputDataSHA256  string                    `json:"input_data_sha256"`
	ComparisonStart  string                    `json:"comparison_start"`
	ComparisonEnd    string                    `json:"comparison_end"`
	Decision         string                    `json:"decision"`
	Reason           string                    `json:"reason"`
	Comparison       *PaperPromotionComparison `json:"comparison,omitempty"`
	SelectionEventID string                    `json:"selection_event_id,omitempty"`
}

type PaperPromotionComparison struct {
	PolicyVersion string                 `json:"policy_version"`
	Windows       []PaperPromotionWindow `json:"windows"`
	Decision      string                 `json:"decision"`
	Reason        string                 `json:"reason"`
}

type PaperPromotionWindow struct {
	Start      string                 `json:"start"`
	End        string                 `json:"end"`
	Candidate  researchdomain.Metrics `json:"candidate"`
	Incumbent  researchdomain.Metrics `json:"incumbent"`
	BuyAndHold researchdomain.Metrics `json:"buy_and_hold"`
}

type PaperPromotionReportView struct {
	SchemaVersion string                      `json:"schema_version"`
	Mode          string                      `json:"mode"`
	ObservedAt    string                      `json:"observed_at"`
	Attempt       *StoredPaperPromotionReport `json:"attempt"`
}

type StoredPaperPromotionReport struct {
	Symbol                   string                          `json:"symbol"`
	ClaimedAt                string                          `json:"claimed_at"`
	DecidedAt                *string                         `json:"decided_at"`
	ComparisonStart          string                          `json:"comparison_start"`
	ComparisonEnd            string                          `json:"comparison_end"`
	Candidate                PaperPromotionReportStrategy    `json:"candidate"`
	Incumbent                PaperPromotionReportStrategy    `json:"incumbent"`
	Decision                 string                          `json:"decision"`
	ReasonCode               string                          `json:"reason_code"`
	PromotionSelectionStatus string                          `json:"promotion_selection_status"`
	Comparison               *PaperPromotionReportComparison `json:"comparison"`
}

type PaperPromotionReportStrategy struct {
	FastWindow int64  `json:"fast_window"`
	SlowWindow int64  `json:"slow_window"`
	Quantity   string `json:"quantity"`
}

type PaperPromotionReportComparison struct {
	PolicyVersion       string                       `json:"policy_version"`
	MetricsVerification string                       `json:"metrics_verification"`
	Decision            string                       `json:"decision"`
	ReasonCode          string                       `json:"reason_code"`
	Windows             []PaperPromotionReportWindow `json:"windows"`
}

type PaperPromotionReportWindow struct {
	Start      string                      `json:"start"`
	End        string                      `json:"end"`
	Candidate  PaperPromotionReportMetrics `json:"candidate"`
	Incumbent  PaperPromotionReportMetrics `json:"incumbent"`
	BuyAndHold PaperPromotionReportMetrics `json:"buy_and_hold"`
}

type PaperPromotionReportMetrics struct {
	AfterCostReturn string `json:"after_cost_return"`
	MaxDrawdown     string `json:"max_drawdown"`
	RoundTrips      string `json:"round_trips"`
	Turnover        string `json:"turnover"`
}

type PaperSignal struct {
	SchemaVersion            string `json:"schema_version"`
	SignalID                 string `json:"signal_id"`
	SignalBarObservationID   string `json:"signal_bar_observation_id,omitempty"`
	StrategyResultSHA256     string `json:"strategy_result_sha256"`
	StrategySelectionEventID string `json:"strategy_selection_event_id"`
	DataSHA256               string `json:"data_sha256"`
	Symbol                   string `json:"symbol"`
	TargetQuantity           string `json:"target_quantity"`
	DataAsOf                 string `json:"data_as_of"`
	GeneratedAt              string `json:"generated_at"`
	ExpiresAt                string `json:"expires_at"`
}

type PaperMarketObservation struct {
	Source            string `json:"source"`
	Symbol            string `json:"symbol"`
	ObservedAt        string `json:"observed_at"`
	AskPrice          string `json:"ask_price"`
	AvailableQuantity string `json:"available_quantity"`
}

type PaperScheduledRunResult struct {
	AccountRef               string `json:"account_ref"`
	AsOf                     string `json:"as_of"`
	PerformanceID            string `json:"performance_id"`
	StrategyPerformanceID    string `json:"strategy_performance_id"`
	PolicyEventID            string `json:"policy_event_id"`
	Decision                 string `json:"decision"`
	ReasonCode               string `json:"reason_code"`
	AutomaticHaltCount       int    `json:"automatic_halt_count"`
	RollbackSelectionEventID string `json:"rollback_selection_event_id,omitempty"`
}

type PaperSnapshotImport struct {
	InputSHA256            string `json:"input_sha256"`
	SignalBarObservationID string `json:"signal_bar_observation_id"`
	Bars                   int    `json:"bars"`
	Added                  int    `json:"added"`
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

type StrategyEvidence struct {
	ResultSHA256    string `json:"result_sha256"`
	ArtifactSHA256  string `json:"artifact_sha256"`
	StrategyName    string `json:"strategy_name"`
	StrategyVersion string `json:"strategy_version"`
	ParameterSHA256 string `json:"parameter_sha256"`
	Target          string `json:"target"`
	RecordedAt      string `json:"recorded_at"`
}

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

type StrategySelectionState struct {
	CurrentEventID       string `json:"current_event_id"`
	SelectedResultSHA256 string `json:"selected_result_sha256"`
}
