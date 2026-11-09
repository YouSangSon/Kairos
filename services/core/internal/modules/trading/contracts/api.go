package contracts

import (
	"context"
)

type Trading interface {
	Orders
	Strategies
	Paper
	Promotions
	Recovery
}

type Orders interface {
	SubmitSyntheticLimit(context.Context, string, int64) (*OrderState, error)
	RecordOrderIntent(ctx context.Context, intent OrderIntent) (*OrderState, error)
	AppendOrderEvent(ctx context.Context, event OrderEvent) (*OrderState, error)
	LoadOrderState(ctx context.Context, orderID string) (*OrderState, error)
	SetSyntheticExecutionArmed(ctx context.Context, accountRef string, armed bool) (*ExecutionAuthorityState, error)
	AcquireSyntheticExecutionLease(ctx context.Context, accountRef string) (*ExecutionAuthorityState, error)
	AuthorizeSyntheticDispatch(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error)
	ReconcileKnownOrder(ctx context.Context, orderID string, lookup OrderLookup) (*OrderReconciliation, error)
	LocalOrderLog(ctx context.Context) (*LocalOrderLog, error)
}

type Strategies interface {
	RegisterEvidence(context.Context, []byte) (*StrategyEvidence, error)
	SelectPaperCandidate(ctx context.Context, resultSHA256, expectedCurrentEventID string) (*StrategySelectionState, error)
	RollbackPaperCandidate(ctx context.Context, expectedCurrentEventID, sourceEventID string) (*StrategySelectionState, error)
}

type LocalPaperRun interface {
	AcceptBundle(context.Context, []byte) (*LocalPaperStepResult, error)
	Heartbeat(context.Context) error
	Close() error
}

type Paper interface {
	BeginLocalPaperRun(account, selection string) LocalPaperRun
	OpenPaperAccountingSession(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperAccountingSession, error)
	EvaluatePaperOperations(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperEvaluationEvent, error)
	ImportPaperSnapshot(ctx context.Context, raw []byte) (*PaperSnapshotImport, error)
	RecordPaperMarketBar(ctx context.Context, input PaperMarketBarObservation) (*PaperMarketBarObservation, error)
	AdmitPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error)
	RunPaperOrder(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error)
	EvaluatePaperPerformance(ctx context.Context, accountRef, asOf string) (*PaperPerformanceEvent, error)
	EvaluatePaperStrategyPerformance(
		ctx context.Context,
		accountRef string,
		expectedSelectionEventID string,
		expectedLatestPerformanceID string,
	) (*PaperStrategyPerformanceEvent, error)
	ApplyPaperPerformancePolicy(ctx context.Context, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID string) (*PaperPerformancePolicyEvent, error)
	PaperMonitor(ctx context.Context) (*PaperMonitor, error)
	PaperInputStatus(ctx context.Context, account, selection string, raw []byte) (*PaperInputStatus, error)
	ExecuteLocalPaper(ctx context.Context, account, selection string, proposalRaw, barsRaw, researchRaw []byte) (result *LocalPaperStepResult, resultErr error)
	RunDuePaperPerformancePolicy(ctx context.Context, accountRef string) (*PaperScheduledRunResult, error)
	RunPaperPerformanceLoop(ctx context.Context, accountRef string) error
	RunPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, observation PaperMarketObservation, fencingToken int64) (*OrderState, error)
	AdmitPaperProposal(ctx context.Context, accountRef, selectionEventID string, raw []byte, barID string, fencingToken int64) (*PaperSignalEvent, *OrderState, error)
}

type Promotions interface {
	PromotePaperCandidate(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*PaperPromotionReceipt, error)
	PaperPromotionStatus(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error)
	AbortPaperPromotion(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error)
	PaperPromotionReport(ctx context.Context) (*PaperPromotionReportView, error)
	ImprovePaperCandidate(ctx context.Context, account, attemptID string, artifact, raw []byte) (*PaperImprovementReceipt, error)
}
