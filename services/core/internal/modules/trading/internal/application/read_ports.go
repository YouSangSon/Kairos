package application

import (
	"context"
)

type ReadUnitOfWork interface {
	Read(context.Context, func(ReadScope) error) error
	InspectRead(context.Context, func(ReadScope) error) error
}

type ReadScope interface {
	Orders() OrderProjectionReads
	Monitor() MonitorProjectionReads
	InputStatus() InputStatusReads
	PromotionReport() PromotionReportReads
	ResearchReport() ResearchReportReads
}

type OrderLogEntry struct{ OrderID, RecordedAt string }

type OrderProjectionReads interface {
	ProveOrders(context.Context) error
	LatestOrders(context.Context) ([]OrderLogEntry, error)
	Order(context.Context, string) (OrderIntent, *OrderState, error)
}

type MonitorProjectionReads interface {
	ProveRunner(context.Context) error
	Registry(context.Context) (StrategyRegistryProof, error)
	SessionCount(context.Context) (int, error)
	PendingPolicyCount(context.Context) (int, error)
	Lease(context.Context) (RunnerLeaseRecord, error)
	LatestPolicy(context.Context) (*PaperPerformancePolicyEvent, *PaperStrategyPerformanceEvent, error)
}

type InputStatusReads interface {
	RequireSchema(context.Context) error
	ProveRunner(context.Context) error
	Registry(context.Context) (StrategyRegistryProof, error)
	SelectionResult(context.Context, string) (string, error)
	ResearchArtifact(context.Context, string) (string, error)
	Session(context.Context, string) (*PaperAccountingSession, bool, error)
	Bars(context.Context, PaperMarketBarObservation, PaperMarketBarObservation) (map[string][]PaperMarketBarObservation, error)
	Signal(context.Context, string, string) (*PaperSignalEvent, bool, error)
	OrderBySignal(context.Context, string, string) (string, bool, error)
	OrderState(context.Context, string) (*OrderState, error)
}

type PromotionReportReads interface {
	RequireSchema(context.Context) error
	Registry(context.Context) (StrategyRegistryProof, error)
	LatestAttemptID(context.Context) (string, bool, error)
	Claim(context.Context, string) (*PromotionClaim, *PromotionDecision, bool, error)
	CandidateArtifact(context.Context, string) (string, error)
}

type ResearchReportReads interface {
	Registry(context.Context) (StrategyRegistryProof, error)
	LatestArtifact(context.Context) (string, string, error)
}
