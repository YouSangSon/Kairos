package contracts

import "time"

type PriceIdentity struct {
	InstrumentID string
	Currency     string
}
type AdmittedPrices struct {
	LedgerRevision string
	Identities     []PriceIdentity
}
type SplitFact struct {
	InstrumentID string
	Currency     string
	OccurredAt   time.Time
}
type ValuationEvent struct{ Type, OccurredAt, InstrumentID, Symbol, Quantity, Currency, Amount, CounterCurrency, CounterAmount string }
type ValuationFacts struct {
	RestatedEvents     []ValuationEvent
	ExternalFlows      []ValuationEvent
	FutureRecorded     bool
	Snapshot           PortfolioSnapshot
	Revision           int64
	ActivityRecordedAt string
	AsOf               time.Time
	RecordedAt         time.Time
	LastSplits         []SplitFact
}
type BackupProof struct {
	Snapshot              PortfolioSnapshot
	BrokerSHA256          string
	BrokerSnapshots       int
	BrokerReconciliations int
}

type BrokerRecovery struct {
	SHA256          string
	Snapshots       int
	Reconciliations int
	LedgerRevision  int64
}
