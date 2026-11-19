package domain

import (
	"math/big"
	"omni-folio/services/core/internal/exact"
	"sort"
	"time"
)

type APIError struct {
	Code    string
	Message string
	Field   string
}
type Money struct {
	Currency string
	Amount   string
}
type Holding struct {
	InstrumentID string
	Symbol       string
	Quantity     string
	CostBasis    string
	Currency     string
}
type FXObservation struct {
	ObservationID       string
	Source              string
	SourceObservationID string
	BaseCurrency        string
	QuoteCurrency       string
	Rate                string
	ObservedAt          string
	FetchedAt           string
	RecordedAt          string
	OwnerVersion        string
	OwnerSample         bool
}
type SecurityPriceObservation struct {
	ObservationID       string
	Source              string
	SourceObservationID string
	InstrumentID        string
	Symbol              string
	Venue               string
	Currency            string
	Price               string
	PriceAdjustment     string
	ObservedAt          string
	FetchedAt           string
	RecordedAt          string
	OwnerVersion        string
	OwnerSample         bool
}
type BenchmarkIndexPoint struct {
	ObservedAt string
	Level      string
}
type BenchmarkSeriesInput struct {
	SchemaVersion   string
	BenchmarkID     string
	Label           string
	Currency        string
	ReturnBasis     string
	SourceReference string
	SourceVersion   string
	Sample          bool
	FetchedAt       string
	Points          []BenchmarkIndexPoint
}
type BenchmarkSeriesReceipt struct {
	StorageCheck    string
	Authority       string
	BenchmarkID     string
	Label           string
	Currency        string
	ReturnBasis     string
	SourceReference string
	SourceVersion   string
	Sample          bool
	DataSHA256      string
	PointCount      int
	FirstObservedAt string
	LastObservedAt  string
	FetchedAt       string
	RecordedAt      string
}
type HoldingValuationPrice struct {
	ObservationID   string
	Source          string
	SourceVersion   string
	Venue           string
	Currency        string
	Price           string
	PriceAdjustment string
	ObservedAt      string
	FetchedAt       string
	RecordedAt      string
	Sample          bool
	State           string
}
type HoldingValuationLine struct {
	InstrumentID  string
	Symbol        string
	Quantity      string
	CostBasis     string
	Currency      string
	Status        string
	Price         *HoldingValuationPrice
	MarketValue   *string
	UnrealizedPnL *string
	Issue         *APIError
}
type HoldingValuationTotal struct {
	Currency      string
	CostBasis     string
	MarketValue   string
	UnrealizedPnL string
}
type HoldingValuation struct {
	Scope                    string
	PolicyVersion            string
	MaxObservationAgeSeconds int64
	ValuationAsOf            string
	LedgerRevision           string
	LedgerAsOf               string
	LedgerRecordedAt         string
	Status                   string
	Sample                   bool
	Totals                   []HoldingValuationTotal
	Lines                    []HoldingValuationLine
	Issues                   []APIError
}
type CashValuationFX struct {
	ObservationID string
	Source        string
	SourceVersion string
	BaseCurrency  string
	QuoteCurrency string
	Rate          string
	ObservedAt    string
	FetchedAt     string
	RecordedAt    string
	Sample        bool
	State         string
}
type CashValuationLine struct {
	Currency     string
	Amount       string
	Status       string
	ValuedAmount *string
	FX           *CashValuationFX
	Issue        *APIError
}
type CashValuation struct {
	Scope                    string
	PolicyVersion            string
	MaxObservationAgeSeconds int64
	BaseCurrency             string
	ValuationAsOf            string
	LedgerRevision           string
	LedgerAsOf               string
	LedgerRecordedAt         string
	Status                   string
	Sample                   bool
	Total                    *Money
	Lines                    []CashValuationLine
	Issues                   []APIError
}
type CurrentPortfolioValuation struct {
	ConversionFX             []CashValuationFX
	Scope                    string
	PolicyVersion            string
	MaxObservationAgeSeconds int64
	BaseCurrency             string
	ValuationAsOf            string
	LedgerRevision           string
	LedgerAsOf               string
	LedgerRecordedAt         string
	Status                   string
	Sample                   bool
	CashValue                *Money
	HoldingsValue            *Money
	TotalValue               *Money
	Issues                   []APIError
}
type PortfolioTWR struct {
	PolicyVersion      string
	Scope              string
	Valuation          *CurrentPortfolioValuation
	Status             string
	Sample             bool
	PeriodStart        *string
	PeriodEnd          string
	ExternalEventCount *int
	BoundaryCount      *int
	CumulativeRate     *string
	NetContributions   *Money
	Issues             []APIError
}
type PortfolioTWRCheckpoint struct {
	At             string
	Kind           string
	CumulativeRate string
}
type PortfolioTWRCheckpoints struct {
	PolicyVersion string
	Report        *PortfolioTWR
	Points        []PortfolioTWRCheckpoint
}
type PortfolioXIRR struct {
	PolicyVersion      string
	SolverPolicy       string
	Scope              string
	Valuation          *CurrentPortfolioValuation
	Status             string
	Sample             bool
	PeriodStart        *string
	PeriodEnd          string
	ExternalEventCount *int
	SignChanges        *int
	Uniqueness         string
	AnnualRate         *string
	CandidateRate      *string
	NetContributions   *Money
	Issues             []APIError
}
type BenchmarkRateResult struct {
	Status string
	Rate   *string
	Issue  *string
}
type BenchmarkFXMark struct {
	ObservedAt    string
	Rate          string
	Source        string
	SourceVersion string
	Sample        *bool
}
type PortfolioBenchmark struct {
	Series                     *BenchmarkSeriesReceipt
	Sample                     bool
	Start                      *BenchmarkIndexPoint
	End                        *BenchmarkIndexPoint
	StartFX                    *BenchmarkFXMark
	EndFX                      *BenchmarkFXMark
	Native                     BenchmarkRateResult
	KRW                        BenchmarkRateResult
	DifferencePercentagePoints *string
}
type PortfolioBenchmarks struct {
	PolicyVersion            string
	MaxObservationAgeSeconds int64
	Portfolio                *PortfolioTWR
	Benchmarks               []PortfolioBenchmark
	Issues                   []APIError
}
type PortfolioSnapshot struct {
	LedgerRevision, AsOf, RecordedAt string
	Cash                             []Money
	Holdings                         []Holding
}
type Transaction struct{ Type, OccurredAt, InstrumentID, Symbol, Quantity, Currency, Amount, CounterCurrency, CounterAmount string }
type SplitFact struct {
	InstrumentID, Currency string
	OccurredAt             time.Time
}
type LedgerFacts struct {
	Snapshot                      PortfolioSnapshot
	AsOf, RecordedAt              time.Time
	FutureRecorded                bool
	LastSplits                    []SplitFact
	RestatedEvents, ExternalFlows []Transaction
}
type BenchmarkVersion struct {
	Sequence               int64
	Input                  BenchmarkSeriesInput
	DataSHA256, RecordedAt string
}
type MarketInputs struct {
	Cutoff                time.Time
	FX                    []FXObservation
	Prices                []SecurityPriceObservation
	Benchmarks            []BenchmarkVersion
	BenchmarkVersionCount int
}
type Inputs struct {
	Cutoff time.Time
	Ledger LedgerFacts
	Market MarketInputs
}
type Failure struct {
	Code, Message, Field string
	Conflict             bool
}

func (e *Failure) Error() string { return e.Message }

const ownerPriceSource = "owner_declared"
const ownerPriceAdjustment = "unadjusted"
const ownerFXSource = "owner_declared"
const marketDataAdjustmentUnspecified = "unspecified"

var parseDecimal = exact.ParseDecimal
var formatDecimal = exact.FormatDecimal
var quantizeHalfEven = exact.QuantizeHalfEven

func addRat(values map[string]*big.Rat, key string, value *big.Rat) {
	if values[key] == nil {
		values[key] = new(big.Rat)
	}
	values[key].Add(values[key], value)
}
func sortedKeys(values map[string]*big.Rat) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
