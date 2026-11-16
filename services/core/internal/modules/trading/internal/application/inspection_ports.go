package application

import (
	"context"
	"time"
)

type AuthorityInspection interface {
	LoadExecutionAuthoritySnapshot(context.Context, string) (AuthoritySnapshot, error)
}
type LocalResearchFacts struct {
	Slow   int
	SHA256 string
}
type InspectionScope interface {
	Authority() AuthorityInspection
	Evaluations() EvaluationInspection
	Selections() SelectionInspection
	Paper() PaperInspection
	Runner() RunnerLeaseReader
	Recovery() RecoveryJournal
	Orders() OrderInspection
}
type EvaluationInspection interface {
	PolicyFrontier(context.Context, string, string) (string, int, error)
	PolicyCloses(context.Context, string, string, string, string, bool, bool) ([]string, error)

	CurrentPaperPerformanceSelection(context.Context) (int64, string, string, error)
	LatestAvailableClose(context.Context, time.Time) (string, error)
	LatestPerformanceClose(context.Context, string) (string, error)
	LoadPaperPerformanceByKey(context.Context, string, string) (*PaperPerformanceEvent, bool, error)
	ScheduledResult(context.Context, string, string) (*PaperScheduledRunResult, bool, error)
}
type SelectionInspection interface {
	LocalResearch(context.Context, string) (LocalResearchFacts, error)
	RecordedSelectionTime(context.Context, string) (string, error)

	ReplayStrategyRegistry(context.Context) (StrategyRegistryProof, error)
	FindEvidence(context.Context, string) (string, string, bool, error)
}
type PaperInspection interface {
	LoadPaperAccountingSession(context.Context, string) (*PaperAccountingSession, bool, error)
}
type OrderInspection interface {
	PaperOrderIDs(context.Context, string) ([]string, error)
	LoadOrderIntentFrom(context.Context, string) (OrderIntent, error)
	LoadOrderStateFrom(context.Context, string) (*OrderState, error)
}

func inspectResult[T any](ctx context.Context, journal UnitOfWork, work func(InspectionScope) (T, error)) (T, error) {
	var result T
	err := journal.Inspect(ctx, func(scope InspectionScope) error { var err error; result, err = work(scope); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
