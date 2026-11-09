package application

import (
	"context"
	"math/big"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"omni-folio/services/core/internal/paperdomain"
	"omni-folio/services/core/internal/researchdomain"
	"time"
)

type UnitOfWork interface {
	Write(context.Context, func(WriteScope) error) error
}

type PromotionReader interface {
	PromotionInputs(context.Context, string, string, string, string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error)
	LoadPromotion(context.Context, string) (*PromotionClaim, *PromotionDecision, bool, error)
}

type WriteScope interface {
	Orders() OrdersJournal
	Authority() AuthorityJournal
	Reservations() ReservationsJournal
	Selections() SelectionsJournal
	Paper() PaperJournal
	Promotions() PromotionsJournal
	Runner() RunnerJournal
	Policy() PolicyJournal
	Recovery() RecoveryJournal
}

type OrdersJournal interface {
	LoadOrderIntentFrom(ctx context.Context, orderID string) (OrderIntent, error)
	LoadOrderStateFrom(ctx context.Context, orderID string) (*OrderState, error)
	RecordOrderIntentTx(ctx context.Context, intent OrderIntent) (*OrderState, error)
	AccountHasUnresolvedOrder(ctx context.Context, accountRef string) (bool, error)
	AppendOrderEventTx(ctx context.Context, event OrderEvent, recordedAt string) (*OrderState, error)
	EventSequence(context.Context, string) (int64, error)
}

type AuthorityJournal interface {
	LoadExecutionAuthorityRecordByID(ctx context.Context, eventID string) (AuthorityRecord, error)
	InsertExecutionAuthorityRecord(ctx context.Context, record AuthorityRecord) error
	AuthoritySequence(context.Context) (int64, error)
	ArmedExecutionAccounts(ctx context.Context) ([]string, error)
	LoadExecutionAuthoritySnapshot(ctx context.Context, accountRef string) (AuthoritySnapshot, error)
	LatestAuthorityID(context.Context, string) (string, error)
}

type ReservationsJournal interface {
	LoadRiskReservationByOrder(ctx context.Context, orderID string) (RiskReservation, bool, error)
	ValidateAuthorizedReservation(ctx context.Context, reservation RiskReservation) (*OrderState, error)
	ActiveReservedNotional(ctx context.Context, accountRef string) (*big.Rat, error)
	InsertRiskReservation(ctx context.Context, record RiskReservation) error
}

type SelectionsJournal interface {
	LoadCurrentStrategyExecutionPolicy(ctx context.Context, resultSHA256, eventID string) (StrategyExecutionPolicy, error)
	ReplayStrategyRegistry(ctx context.Context) (StrategyRegistryProof, error)
	SelectionSequence(context.Context) (int64, error)
	ValidateStrategyOrderSelection(ctx context.Context, intent OrderIntent) error
	LatestPaperEvaluationSequence(ctx context.Context) (int64, error)
	InsertStrategySelectionEvent(ctx context.Context, event StrategySelectionEvent) error
}

type PaperJournal interface {
	RecordPaperSignalEventTx(ctx context.Context, accountRef string, signal PaperSignal) (*PaperSignalEvent, error)
	PaperOrderBySignalFrom(ctx context.Context, accountRef, signalID string) (string, bool, error)
	ValidateCapitalizedPaperOrderBindings(ctx context.Context, intent OrderIntent) (*PaperAccountingSession, *PaperSignalEvent, error)
	ReplayPaperProjectionFrom(ctx context.Context, accountRef, symbol string) (Projection, error)
	LoadPaperExecutionAuthorizationByOrder(ctx context.Context, orderID string) (PaperAuthorization, bool, error)
	ValidatePaperExecutionAuthorization(ctx context.Context, authorization PaperAuthorization) (*OrderState, error)
	InsertPaperExecutionAuthorization(ctx context.Context, authorization PaperAuthorization) error
	ReplayPaperAccounting(ctx context.Context) (map[string]PaperAccountState, error)
	PaperFillBars(ctx context.Context, signal PaperSignalEvent, delay int64) ([]*PaperMarketBarObservation, error)
	PaperOrderUsedFillBars(ctx context.Context, orderID string) (map[string]bool, error)
	PaperConsumedBarCapacity(ctx context.Context, accountRef, symbol, observationID string) (*big.Int, error)
	AppendCapitalizedPaperFillTx(ctx context.Context, event OrderEvent, recordedAt string) (*OrderState, error)
	LoadPaperAccountingSession(ctx context.Context, accountRef string) (*PaperAccountingSession, bool, error)
	RequireIsolatedPaperAccount(ctx context.Context, account string) error
	ValidatePaperAccountingSession(PaperAccountingSession) (StrategyExecutionPolicy, error)
}

type PromotionsJournal interface {
	LoadPaperPromotion(context.Context, string) (*PromotionClaim, *PromotionDecision, bool, error)
	PendingPromotionCount(context.Context) (int, error)
	PromotionInputs(context.Context, string, string, string, string) (researchdomain.Strategy, researchdomain.Strategy, researchdomain.Execution, error)
	OrderSequence(context.Context) (int64, error)
	ResearchSequence(context.Context) (int64, error)
	PreviousPromotionAttempt(context.Context) (string, error)
	PromotionDecisionTime(context.Context, string) (string, error)
	ValidatePromotionClaim(context.Context, PromotionClaim, int64) error
	InsertPromotionClaim(context.Context, PromotionClaim) error
	InsertPromotionDecision(context.Context, PromotionDecision) error
	CapturePromotionState(context.Context, string, time.Time) (string, *PaperAccountingSession, string, StrategyRegistryProof, error)
}

type RunnerJournal interface {
	ValidatePaperRunnerLeaseTx(ctx context.Context, claim *RunnerClaim, accountRef, ownerID string, now time.Time) error
	RenewPaperRunnerLeaseTx(ctx context.Context, claim *RunnerClaim, ownerID string, now time.Time) (*RunnerClaim, error)
	RenewPaperRunnerLeaseAfterPolicyRollbackTx(ctx context.Context, claim *RunnerClaim, policyEventID, rollbackEventID, ownerID string, now time.Time) (*RunnerClaim, error)
}

type PolicyJournal interface {
	LoadPaperPerformancePolicyByKey(ctx context.Context, accountRef, selectionEventID, strategyPerformanceID string) (*PaperPerformancePolicyEvent, bool, error)
	LoadPaperStrategyPerformanceByID(ctx context.Context, id string) (PaperStrategyPerformanceEvent, int64, error)
	LatestPaperPerformancePolicyID(ctx context.Context, accountRef, selectionEventID string) (string, error)
	StrategyPerformanceSequence(context.Context) (int64, error)
	InsertPaperPerformancePolicyEvent(ctx context.Context, event PaperPerformancePolicyEvent) error
	LatestStrategyPerformanceID(context.Context, string) (string, error)
}

type RecoveryJournal interface {
	RequireSchema(context.Context) error
	ProvePaperPromotionRecovery(context.Context) (contracts.PromotionRecovery, error)
	ProvePaperAccountingRecovery(ctx context.Context) (PaperAccountingRecoveryProof, error)
	ProvePaperRunnerLeaseRecovery(ctx context.Context) (PaperRunnerLeaseRecoveryProof, error)
	ProvePaperPerformancePolicyRecovery(ctx context.Context) (PaperPerformancePolicyRecoveryProof, error)
}

type PaperAccountState = paperdomain.AccountState

type PaperAccountingRecoveryProof struct {
	SHA256                                                          string
	Sessions, MarketBars, Signals, Authorizations, CapitalizedFills int
}

type PaperPerformancePolicyRecoveryProof struct {
	SHA256         string
	Events         int
	Actions        int
	AutomaticHalts int
}

type PaperRunnerLeaseRecoveryProof struct {
	SHA256 string
	Leases int
	Active int
}
