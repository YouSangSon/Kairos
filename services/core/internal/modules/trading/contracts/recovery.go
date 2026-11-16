package contracts

import (
	"context"
	storage "omni-folio/services/core/internal/storage/contracts"
)

type BackupQuery struct {
	Schema   int
	Archival bool
}
type Recovery interface {
	VerifyStartup(context.Context, storage.ObservationRef) error
	VerifyRecovery(context.Context) (*RecoverySnapshot, error)
	MigrationFacts(context.Context, storage.ObservationRef, MigrationQuery) (*MigrationSnapshot, error)
	VerifyBackup(context.Context, storage.ObservationRef, BackupQuery) (*RecoverySnapshot, error)
}
type RecoverySnapshot struct {
	Orders              OrderRecovery
	Strategy            StrategyRecovery
	Accounting          PaperAccountingRecovery
	Performance         PaperPerformanceRecovery
	StrategyPerformance PaperStrategyPerformanceRecovery
	Policy              PaperPerformancePolicyRecovery
	Runner              RunnerLeaseRecovery
	Promotion           PromotionRecovery
}

type OrderRecovery struct {
	SHA256                   string
	Orders                   int
	Events                   int
	ExecutionAuthoritySHA256 string
	ExecutionAuthorityEvents int
	RiskReservationSHA256    string
	RiskReservations         int
}

type StrategyRecovery struct {
	SHA256               string
	Evidence             int
	Events               int
	Evaluations          int
	SelectedResultSHA256 string
	CurrentEventID       string
}

type PaperAccountingRecovery struct {
	SHA256                                                          string
	Sessions, MarketBars, Signals, Authorizations, CapitalizedFills int
}

type PaperPerformanceRecovery struct {
	SHA256 string
	Events int
	Marks  int
}

type PaperStrategyPerformanceRecovery struct {
	SHA256  string
	Events  int
	Samples int
}

type PaperPerformancePolicyRecovery struct {
	SHA256         string
	Events         int
	Actions        int
	AutomaticHalts int
}

type RunnerLeaseRecovery struct {
	SHA256 string
	Leases int
	Active int
}

type PromotionRecovery struct {
	SHA256            string
	Claims, Decisions int
}

type MigrationQuery struct {
	Schema      int
	PostRebuild bool
}
type JournalShape struct {
	Rows        int
	MaxSequence int64
}
type MigrationSnapshot struct {
	AuthoritySHA       string
	AuthorityCount     int
	AuthorityShape     JournalShape
	Strategy           StrategyRecovery
	StrategyShape      JournalShape
	Orders             OrderRecovery
	SelectionShape     JournalShape
	AddedPromotionRows int
}
