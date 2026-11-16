package application

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

type StrategyWindowReads interface {
	WindowSelectionTime(context.Context, string, string) (string, error)
	VisitWindowSamples(context.Context, string, string, string, int64, func(PaperPerformanceEvent) error) error
}

func LoadStrategyPerformanceWindow(ctx context.Context, q StrategyWindowReads, accountRef, sessionID, selectionEventID,
	selectedResult, latestPerformanceID string, latestSequence int64,
) (StrategyPerformanceWindow, error) {
	selectionRecordedAt, err := q.WindowSelectionTime(ctx, selectionEventID, selectedResult)
	if err != nil {
		return StrategyPerformanceWindow{}, err
	}
	selection, err := domain.NewStrategyWindowSelection(selectionRecordedAt, selectedResult, latestPerformanceID)
	if err != nil {
		return StrategyPerformanceWindow{}, err
	}
	err = q.VisitWindowSamples(ctx, accountRef, sessionID, selectionEventID, latestSequence, func(event PaperPerformanceEvent) error {
		return selection.Consider(domain.PaperPerformanceEvent(event))
	})
	if err != nil {
		return StrategyPerformanceWindow{}, err
	}
	return selection.Window()
}
