package application

import (
	"context"
	"errors"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

type PerformanceRecoveryProof struct {
	SHA256        string
	Events, Marks int
}
type StrategyPerformanceRecoveryProof struct {
	SHA256          string
	Events, Samples int
}
type EvaluationsJournal interface {
	OperationalSnapshot(context.Context, string, string, string) (OperationalSnapshot, error)
	LoadEvaluation(context.Context, string) (*PaperEvaluationEvent, bool, error)
	LatestEvaluationID(context.Context, string, string) (string, error)
	InsertEvaluation(context.Context, PaperEvaluationEvent) error
	ProvePaperPerformanceRecovery(context.Context) (PerformanceRecoveryProof, error)
	LoadPaperPerformanceByKey(context.Context, string, string) (*PaperPerformanceEvent, bool, error)
	RejectLegacyPaperPerformanceAccount(context.Context, string) error
	CurrentPaperPerformanceSelection(context.Context) (int64, string, string, error)
	Cutoffs(context.Context) (int64, int64, error)
	ReplayPaperAccountingStateWithCutoff(context.Context, int64, string) (map[string]PaperAccountState, error)
	DerivePaperPerformanceMarks(context.Context, PaperAccountState, string, string, string, int64) ([]PerformanceMark, paperdomain.Valuation, error)
	PriorPaperPerformanceSeries(context.Context, string, string, string) (string, []string, error)
	InsertPaperPerformanceEvent(context.Context, PaperPerformanceEvent) error
	ProvePaperStrategyPerformanceRecovery(context.Context) (StrategyPerformanceRecoveryProof, error)
	LoadLatestPaperPerformance(context.Context, string) (PaperPerformanceEvent, int64, bool, error)
	LoadPaperStrategyPerformanceByKey(context.Context, string, string, string) (*PaperStrategyPerformanceEvent, bool, error)
	LoadPaperStrategyPerformanceWindow(context.Context, string, string, string, string, string, int64) (StrategyPerformanceWindow, error)
	LatestPaperStrategyPerformanceID(context.Context, string, string, string) (string, error)
	InsertPaperStrategyPerformanceEvent(context.Context, PaperStrategyPerformanceEvent) error
}

var ErrPaperRunnerPriorSelection = errors.New("scheduled paper runner close belongs to a prior strategy selection")
