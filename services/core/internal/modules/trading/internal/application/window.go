package application

import (
	"context"
	"errors"
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
	selectionTime, ok := canonicalUTCTime(selectionRecordedAt)
	if !ok {
		return StrategyPerformanceWindow{}, errors.New("paper strategy performance selection time is invalid")
	}
	var window StrategyPerformanceWindow
	foundLatest := false
	err = q.VisitWindowSamples(ctx, accountRef, sessionID, selectionEventID, latestSequence, func(event PaperPerformanceEvent) error {
		asOf, ok := parsePaperTime(event.AsOf)
		if !ok || asOf.Before(selectionTime) {
			return nil
		}
		if event.SelectedStrategyResultRef != selectedResult {
			return errors.New("paper strategy performance result binding is invalid")
		}
		if len(window.Equities) == 0 {
			window.Baseline = event
		}
		window.Latest = event
		window.Equities = append(window.Equities, event.Equity)
		if event.PerformanceID == latestPerformanceID {
			foundLatest = true
		}
		return nil
	})
	if err != nil {
		return StrategyPerformanceWindow{}, err
	}
	if !foundLatest || len(window.Equities) == 0 || window.Latest.PerformanceID != latestPerformanceID {
		return StrategyPerformanceWindow{}, errors.New("paper strategy performance current-selection window is missing")
	}
	return window, nil
}
