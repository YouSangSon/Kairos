package contracts

import (
	"context"
)

type BackupQuery struct {
	Schema   int
	Archival bool
}
type Recovery interface {
	VerifyBackup(context.Context, BackupQuery) (*RecoverySnapshot, error)
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
