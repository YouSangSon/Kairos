package application

import (
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

type RunnerClaim struct {
	Scope                    string
	FencingToken             int64
	OwnerID                  string
	AccountRef               string
	HeartbeatAtNS            int64
	LeaseExpiresAtNS         int64
	StrategySelectionEventID string
	SelectedResultSHA256     string
}

type AuthorityRecord = domain.AuthorityRecord

type RiskReservation = domain.RiskReservation

type PaperAuthorization = domain.PaperAuthorization

type StrategyExecutionPolicy struct {
	StartingCash, Fee, Tax, SlippageBPS, MaxParticipation string
	DelayBars                                             int64
	SignalPrice, FillPrice, SHA256                        string
	CanonicalJSON                                         string `json:"-"`
}

type StrategyRegistryProof struct {
	SHA256               string
	Evidence             int
	Events               int
	Evaluations          int
	SelectedResultSHA256 string
	CurrentEventID       string
	Stack                []string `json:"-"`
}

type Projection = domain.Projection

type AuthoritySnapshot struct {
	contracts.ExecutionAuthorityState
	EventID string
	Count   int `json:"-"`
}

type paperRunnerClaim = RunnerClaim

type executionAuthorityRecord = AuthorityRecord

type riskReservationRecord = RiskReservation

type paperExecutionAuthorization = PaperAuthorization

type strategyExecutionPolicy = StrategyExecutionPolicy

type strategyRegistryRecoveryProof = StrategyRegistryProof

type paperProjection = Projection

type executionAuthoritySnapshot = AuthoritySnapshot
