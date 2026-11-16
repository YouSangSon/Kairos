package domain

import (
	"errors"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	"sort"
	"time"
)

var ErrPaperRunnerIncompleteMarks = errors.New("paper performance marks are incomplete")

type selectedMark struct {
	bar      *PaperMarketBarObservation
	sequence int64
}
type PerformanceMarkSelection struct {
	positions map[string]bool
	selected  map[string]selectedMark
	recorded  time.Time
	asOf      string
}

func NewPerformanceMarkSelection(state paperdomain.AccountState, asOf, recordedAt string, marketCutoff int64) (*PerformanceMarkSelection, error) {
	if marketCutoff < 0 || !canonicalPaperTimes(asOf, recordedAt) {
		return nil, errors.New("paper performance mark cutoff is invalid")
	}
	recorded, _ := parsePaperTime(recordedAt)
	positions := make(map[string]bool, len(state.Lots))
	for symbol, lots := range state.Lots {
		if len(lots) != 0 {
			positions[symbol] = true
		}
	}
	return &PerformanceMarkSelection{positions: positions, selected: make(map[string]selectedMark, len(positions)), recorded: recorded, asOf: asOf}, nil
}
func (s *PerformanceMarkSelection) NeedsMarks() bool { return len(s.positions) != 0 }
func CashOnlyPerformanceValuation(anchored int, startingCash string, state paperdomain.AccountState) ([]PerformanceMark, paperdomain.Valuation, error) {
	if anchored == 0 {
		return nil, paperdomain.Valuation{}, errors.New("paper performance cash-only close anchor is missing")
	}
	valuation, err := paperdomain.ValueAccount(startingCash, state, map[string]string{})
	if err != nil {
		return nil, paperdomain.Valuation{}, err
	}
	return []PerformanceMark{}, valuation, nil
}
func (s *PerformanceMarkSelection) Consider(bar *PaperMarketBarObservation, sequence int64) error {
	if !s.positions[bar.Symbol] {
		return nil
	}
	if bar.Source != "paper_fixture" || bar.Venue != "KRX" || bar.Currency != "KRW" || bar.Interval != "1d" ||
		bar.Timezone != "Asia/Seoul" || bar.PriceAdjustment != "unspecified" || bar.CloseAt != s.asOf {
		return nil
	}
	sourceAvailable, _ := parsePaperTime(bar.SourceAvailableAt)
	fetched, _ := parsePaperTime(bar.FetchedAt)
	barRecorded, _ := parsePaperTime(bar.RecordedAt)
	if sourceAvailable.After(s.recorded) || fetched.After(s.recorded) || barRecorded.After(s.recorded) {
		return errors.New("paper performance mark was unavailable at record time")
	}
	if _, exists := s.selected[bar.Symbol]; exists {
		return errors.New("paper performance mark is ambiguous")
	}
	s.selected[bar.Symbol] = selectedMark{bar: bar, sequence: sequence}
	return nil
}
func (s *PerformanceMarkSelection) Value(startingCash string, state paperdomain.AccountState) ([]PerformanceMark, paperdomain.Valuation, error) {
	if len(s.selected) != len(s.positions) {
		return nil, paperdomain.Valuation{}, ErrPaperRunnerIncompleteMarks
	}

	closes := make(map[string]string, len(s.selected))
	for symbol, mark := range s.selected {
		closes[symbol] = mark.bar.Close
	}
	valuation, err := paperdomain.ValueAccount(startingCash, state, closes)
	if err != nil {
		return nil, paperdomain.Valuation{}, err
	}
	symbols := make([]string, 0, len(valuation.Positions))
	for symbol := range valuation.Positions {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	marks := make([]PerformanceMark, 0, len(symbols))
	for _, symbol := range symbols {
		mark, exists := s.selected[symbol]
		if !exists {
			return nil, paperdomain.Valuation{}, errors.New("paper performance marks do not match positions")
		}
		position := valuation.Positions[symbol]
		marks = append(marks, PerformanceMark{
			Symbol: symbol, Quantity: position.Quantity, ObservationID: mark.bar.ObservationID, ObservationSequence: mark.sequence,
			Close: mark.bar.Close, OpenCost: position.OpenCost, MarketValue: position.MarketValue, UnrealizedPnL: position.UnrealizedPnL,
		})
	}
	return marks, valuation, nil
}
