package contracts

import (
	ledger "omni-folio/services/core/internal/modules/ledger/contracts"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
)

type APIError = ledger.APIError
type Money = ledger.Money
type BenchmarkSeriesReceipt = market.BenchmarkSeriesReceipt
type BenchmarkIndexPoint = market.BenchmarkIndexPoint
type HoldingValuationPrice struct {
	ObservationID   string `json:"observation_id"`
	Source          string `json:"source"`
	SourceVersion   string `json:"source_version,omitempty"`
	Venue           string `json:"venue"`
	Currency        string `json:"currency"`
	Price           string `json:"price"`
	PriceAdjustment string `json:"price_adjustment"`
	ObservedAt      string `json:"observed_at"`
	FetchedAt       string `json:"fetched_at"`
	RecordedAt      string `json:"recorded_at"`
	Sample          bool   `json:"sample"`
	State           string `json:"state"`
}

type HoldingValuationLine struct {
	InstrumentID  string                 `json:"instrument_id"`
	Symbol        string                 `json:"symbol"`
	Quantity      string                 `json:"quantity"`
	CostBasis     string                 `json:"cost_basis"`
	Currency      string                 `json:"currency"`
	Status        string                 `json:"status"`
	Price         *HoldingValuationPrice `json:"price"`
	MarketValue   *string                `json:"market_value"`
	UnrealizedPnL *string                `json:"unrealized_pnl"`
	Issue         *APIError              `json:"issue"`
}

type HoldingValuationTotal struct {
	Currency      string `json:"currency"`
	CostBasis     string `json:"cost_basis"`
	MarketValue   string `json:"market_value"`
	UnrealizedPnL string `json:"unrealized_pnl"`
}

type HoldingValuation struct {
	Scope                    string                  `json:"scope"`
	PolicyVersion            string                  `json:"policy_version"`
	MaxObservationAgeSeconds int64                   `json:"max_observation_age_seconds"`
	ValuationAsOf            string                  `json:"valuation_as_of"`
	LedgerRevision           string                  `json:"ledger_revision"`
	LedgerAsOf               string                  `json:"ledger_as_of"`
	LedgerRecordedAt         string                  `json:"ledger_recorded_at"`
	Status                   string                  `json:"status"`
	Sample                   bool                    `json:"sample"`
	Totals                   []HoldingValuationTotal `json:"totals"`
	Lines                    []HoldingValuationLine  `json:"lines"`
	Issues                   []APIError              `json:"issues"`
}

type CashValuationFX struct {
	ObservationID string `json:"observation_id"`
	Source        string `json:"source"`
	SourceVersion string `json:"source_version,omitempty"`
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Rate          string `json:"rate"`
	ObservedAt    string `json:"observed_at"`
	FetchedAt     string `json:"fetched_at"`
	RecordedAt    string `json:"recorded_at"`
	Sample        bool   `json:"sample"`
	State         string `json:"state"`
}

type CashValuationLine struct {
	Currency     string           `json:"currency"`
	Amount       string           `json:"amount"`
	Status       string           `json:"status"`
	ValuedAmount *string          `json:"valued_amount"`
	FX           *CashValuationFX `json:"fx"`
	Issue        *APIError        `json:"issue"`
}

type CashValuation struct {
	Scope                    string              `json:"scope"`
	PolicyVersion            string              `json:"policy_version"`
	MaxObservationAgeSeconds int64               `json:"max_observation_age_seconds"`
	BaseCurrency             string              `json:"base_currency"`
	ValuationAsOf            string              `json:"valuation_as_of"`
	LedgerRevision           string              `json:"ledger_revision"`
	LedgerAsOf               string              `json:"ledger_as_of"`
	LedgerRecordedAt         string              `json:"ledger_recorded_at"`
	Status                   string              `json:"status"`
	Sample                   bool                `json:"sample"`
	Total                    *Money              `json:"total"`
	Lines                    []CashValuationLine `json:"lines"`
	Issues                   []APIError          `json:"issues"`
}

type CurrentPortfolioValuation struct {
	Scope                    string     `json:"scope"`
	PolicyVersion            string     `json:"policy_version"`
	MaxObservationAgeSeconds int64      `json:"max_observation_age_seconds"`
	BaseCurrency             string     `json:"base_currency"`
	ValuationAsOf            string     `json:"valuation_as_of"`
	LedgerRevision           string     `json:"ledger_revision"`
	LedgerAsOf               string     `json:"ledger_as_of"`
	LedgerRecordedAt         string     `json:"ledger_recorded_at"`
	Status                   string     `json:"status"`
	Sample                   bool       `json:"sample"`
	CashValue                *Money     `json:"cash_value"`
	HoldingsValue            *Money     `json:"holdings_value"`
	TotalValue               *Money     `json:"total_value"`
	Issues                   []APIError `json:"issues"`
}

type PortfolioTWR struct {
	PolicyVersion      string                     `json:"policy_version"`
	Scope              string                     `json:"scope"`
	Valuation          *CurrentPortfolioValuation `json:"valuation"`
	Status             string                     `json:"status"`
	Sample             bool                       `json:"sample"`
	PeriodStart        *string                    `json:"period_start"`
	PeriodEnd          string                     `json:"period_end"`
	ExternalEventCount *int                       `json:"external_event_count"`
	BoundaryCount      *int                       `json:"boundary_count"`
	CumulativeRate     *string                    `json:"cumulative_rate"`
	NetContributions   *Money                     `json:"net_contributions"`
	Issues             []APIError                 `json:"issues"`
}

type PortfolioTWRCheckpoint struct {
	At             string `json:"at"`
	Kind           string `json:"kind"`
	CumulativeRate string `json:"cumulative_rate"`
}

type PortfolioTWRCheckpoints struct {
	PolicyVersion string                   `json:"policy_version"`
	Report        *PortfolioTWR            `json:"report"`
	Points        []PortfolioTWRCheckpoint `json:"points"`
}

type PortfolioXIRR struct {
	PolicyVersion      string                     `json:"policy_version"`
	SolverPolicy       string                     `json:"solver_policy"`
	Scope              string                     `json:"scope"`
	Valuation          *CurrentPortfolioValuation `json:"valuation"`
	Status             string                     `json:"status"`
	Sample             bool                       `json:"sample"`
	PeriodStart        *string                    `json:"period_start"`
	PeriodEnd          string                     `json:"period_end"`
	ExternalEventCount *int                       `json:"external_event_count"`
	SignChanges        *int                       `json:"sign_changes"`
	Uniqueness         string                     `json:"uniqueness"`
	AnnualRate         *string                    `json:"annual_rate"`
	CandidateRate      *string                    `json:"candidate_rate"`
	NetContributions   *Money                     `json:"net_contributions"`
	Issues             []APIError                 `json:"issues"`
}

type BenchmarkRateResult struct {
	Status string  `json:"status"`
	Rate   *string `json:"rate"`
	Issue  *string `json:"issue"`
}

type BenchmarkFXMark struct {
	ObservedAt    string `json:"observed_at"`
	Rate          string `json:"rate"`
	Source        string `json:"source,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
	Sample        *bool  `json:"sample,omitempty"`
}

type PortfolioBenchmark struct {
	Series                     *BenchmarkSeriesReceipt `json:"series"`
	Sample                     bool                    `json:"sample"`
	Start                      *BenchmarkIndexPoint    `json:"start"`
	End                        *BenchmarkIndexPoint    `json:"end"`
	StartFX                    *BenchmarkFXMark        `json:"start_fx"`
	EndFX                      *BenchmarkFXMark        `json:"end_fx"`
	Native                     BenchmarkRateResult     `json:"native"`
	KRW                        BenchmarkRateResult     `json:"krw"`
	DifferencePercentagePoints *string                 `json:"difference_percentage_points"`
}

type PortfolioBenchmarks struct {
	PolicyVersion            string               `json:"policy_version"`
	MaxObservationAgeSeconds int64                `json:"max_observation_age_seconds"`
	Portfolio                *PortfolioTWR        `json:"portfolio"`
	Benchmarks               []PortfolioBenchmark `json:"benchmarks"`
	Issues                   []APIError           `json:"issues"`
}
