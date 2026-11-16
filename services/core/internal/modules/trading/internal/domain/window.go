package domain

import (
	"errors"
	"time"
)

type StrategyPerformanceWindow struct {
	Baseline, Latest PaperPerformanceEvent
	Equities         []string
}
type StrategyWindowSelection struct {
	selectionTime                       time.Time
	selectedResult, latestPerformanceID string
	window                              StrategyPerformanceWindow
	foundLatest                         bool
}

func NewStrategyWindowSelection(selectionRecordedAt, selectedResult, latestPerformanceID string) (*StrategyWindowSelection, error) {
	selectionTime, ok := CanonicalUTCTime(selectionRecordedAt)
	if !ok {
		return nil, errors.New("paper strategy performance selection time is invalid")
	}
	return &StrategyWindowSelection{selectionTime: selectionTime, selectedResult: selectedResult, latestPerformanceID: latestPerformanceID}, nil
}
func (s *StrategyWindowSelection) Consider(event PaperPerformanceEvent) error {
	asOf, ok := parsePaperTime(event.AsOf)
	if !ok || asOf.Before(s.selectionTime) {
		return nil
	}
	if event.SelectedStrategyResultRef != s.selectedResult {
		return errors.New("paper strategy performance result binding is invalid")
	}
	if len(s.window.Equities) == 0 {
		s.window.Baseline = event
	}
	s.window.Latest = event
	s.window.Equities = append(s.window.Equities, event.Equity)
	if event.PerformanceID == s.latestPerformanceID {
		s.foundLatest = true
	}
	return nil
}
func (s *StrategyWindowSelection) Window() (StrategyPerformanceWindow, error) {
	if !s.foundLatest || len(s.window.Equities) == 0 || s.window.Latest.PerformanceID != s.latestPerformanceID {
		return StrategyPerformanceWindow{}, errors.New("paper strategy performance current-selection window is missing")
	}
	return s.window, nil
}
