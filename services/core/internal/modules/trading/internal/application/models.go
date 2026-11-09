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

type AuthorityRecord struct {
	EventID                       string `json:"event_id"`
	AccountRef                    string `json:"account_ref"`
	Armed                         bool   `json:"armed"`
	LeaseOwner                    string `json:"lease_owner,omitempty"`
	FencingToken                  int64  `json:"fencing_token"`
	LeaseExpiresAt                string `json:"lease_expires_at,omitempty"`
	ReasonCode                    string `json:"reason_code"`
	PaperPerformancePolicyEventID string `json:"paper_performance_policy_event_id,omitempty"`
	RecordedAt                    string `json:"recorded_at"`
}

type RiskReservation struct {
	ReservationID    string `json:"reservation_id"`
	OrderID          string `json:"order_id"`
	AccountRef       string `json:"account_ref"`
	PolicyVersion    string `json:"policy_version"`
	AuthorityEventID string `json:"authority_event_id"`
	FencingToken     int64  `json:"fencing_token"`
	Quantity         string `json:"quantity"`
	LimitPrice       string `json:"limit_price"`
	LimitNotional    string `json:"limit_notional"`
	RiskEventID      string `json:"risk_event_id"`
	DispatchEventID  string `json:"dispatch_event_id"`
	ReservedAt       string `json:"reserved_at"`
}

type PaperAuthorization struct {
	AuthorizationID          string `json:"authorization_id"`
	SchemaVersion            string `json:"schema_version"`
	OrderID                  string `json:"order_id"`
	AccountRef               string `json:"account_ref"`
	PaperAccountingSessionID string `json:"paper_accounting_session_id"`
	ExecutionPolicySHA256    string `json:"execution_policy_sha256"`
	PolicyVersion            string `json:"policy_version"`
	Side                     string `json:"side"`
	Quantity                 string `json:"quantity"`
	AuthorityEventID         string `json:"authority_event_id"`
	FencingToken             int64  `json:"fencing_token"`
	RiskEventID              string `json:"risk_event_id"`
	DispatchEventID          string `json:"dispatch_event_id"`
	AuthorizedAt             string `json:"authorized_at"`
}

type StrategyExecutionPolicy struct {
	StartingCash, Fee, Tax, SlippageBPS, MaxParticipation string
	DelayBars                                             int64
	SignalPrice, FillPrice, SHA256, CanonicalJSON         string
}

type StrategyRegistryProof struct {
	SHA256               string
	Evidence             int
	Events               int
	Evaluations          int
	SelectedResultSHA256 string
	CurrentEventID       string
	Stack                []string
}

type Projection = domain.Projection

type AuthoritySnapshot struct {
	contracts.ExecutionAuthorityState
	EventID string
	Count   int
}

type paperRunnerClaim = RunnerClaim

type executionAuthorityRecord = AuthorityRecord

type riskReservationRecord = RiskReservation

type paperExecutionAuthorization = PaperAuthorization

type strategyExecutionPolicy = StrategyExecutionPolicy

type strategyRegistryRecoveryProof = StrategyRegistryProof

type paperProjection = Projection

type executionAuthoritySnapshot = AuthoritySnapshot
