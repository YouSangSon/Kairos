package contracts

type ResearchReportView struct {
	SchemaVersion       string                `json:"schema_version"`
	Mode                string                `json:"mode"`
	MetricsVerification string                `json:"metrics_verification"`
	ObservedAt          string                `json:"observed_at"`
	Report              *StoredResearchReport `json:"report"`
}

type StoredResearchReport struct {
	RecordedAt      string                `json:"recorded_at"`
	FastWindow      int64                 `json:"fast_window"`
	SlowWindow      int64                 `json:"slow_window"`
	Quantity        string                `json:"quantity"`
	CandidateCount  int                   `json:"candidate_count"`
	FoldCount       int                   `json:"fold_count"`
	FinalHoldout    ResearchReportRange   `json:"final_holdout"`
	PromotionTarget string                `json:"promotion_target"`
	FailedGates     []string              `json:"failed_gates"`
	WalkForward     ResearchReportMetrics `json:"walk_forward"`
	Holdout         ResearchReportMetrics `json:"holdout"`
	BuyAndHold      ResearchReportMetrics `json:"buy_and_hold"`
}

type ResearchReportRange struct {
	FirstBarAt string `json:"first_bar_at"`
	LastBarAt  string `json:"last_bar_at"`
	BarCount   int64  `json:"bar_count"`
}

type ResearchReportMetrics struct {
	AfterCostReturn string `json:"after_cost_return"`
	MaxDrawdown     string `json:"max_drawdown"`
	RoundTrips      string `json:"round_trips"`
}
