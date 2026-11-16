package application

import (
	"context"
	"errors"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
	"sort"
)

type PerformanceMarkReader interface {
	CashCloseAnchor(context.Context, int64, string, string) (int, error)
	MarkObservationIDs(context.Context, int64, string) ([]string, error)
	LoadMarkBar(context.Context, string) (*PaperMarketBarObservation, int64, error)
}

func DerivePaperPerformanceMarks(ctx context.Context, q PerformanceMarkReader, state PaperAccountState, startingCash, asOf, recordedAt string, marketCutoff int64) ([]PerformanceMark, paperdomain.Valuation, error) {
	if marketCutoff < 0 || !canonicalPaperTimes(asOf, recordedAt) {
		return nil, paperdomain.Valuation{}, errors.New("paper performance mark cutoff is invalid")
	}
	recorded, _ := parsePaperTime(recordedAt)
	positions := make(map[string]bool, len(state.Lots))
	for symbol, lots := range state.Lots {
		if len(lots) != 0 {
			positions[symbol] = true
		}
	}
	if len(positions) == 0 {
		anchored, err := q.CashCloseAnchor(ctx, marketCutoff, asOf, recordedAt)
		if err != nil {
			return nil, paperdomain.Valuation{}, err
		}
		if anchored == 0 {
			return nil, paperdomain.Valuation{}, errors.New("paper performance cash-only close anchor is missing")
		}
		valuation, err := paperdomain.ValueAccount(startingCash, state, map[string]string{})
		if err != nil {
			return nil, paperdomain.Valuation{}, err
		}
		return []PerformanceMark{}, valuation, nil
	}

	observationIDs, err := q.MarkObservationIDs(ctx, marketCutoff, asOf)
	if err != nil {
		return nil, paperdomain.Valuation{}, err
	}
	type selectedMark struct {
		bar      *PaperMarketBarObservation
		sequence int64
	}
	selected := make(map[string]selectedMark, len(positions))
	for _, observationID := range observationIDs {
		bar, sequence, err := q.LoadMarkBar(ctx, observationID)
		if err != nil {
			return nil, paperdomain.Valuation{}, err
		}
		if !positions[bar.Symbol] {
			continue
		}
		if bar.Source != "paper_fixture" || bar.Venue != "KRX" || bar.Currency != "KRW" || bar.Interval != "1d" ||
			bar.Timezone != "Asia/Seoul" || bar.PriceAdjustment != "unspecified" || bar.CloseAt != asOf {
			continue
		}
		sourceAvailable, _ := parsePaperTime(bar.SourceAvailableAt)
		fetched, _ := parsePaperTime(bar.FetchedAt)
		barRecorded, _ := parsePaperTime(bar.RecordedAt)
		if sourceAvailable.After(recorded) || fetched.After(recorded) || barRecorded.After(recorded) {
			return nil, paperdomain.Valuation{}, errors.New("paper performance mark was unavailable at record time")
		}
		if _, exists := selected[bar.Symbol]; exists {
			return nil, paperdomain.Valuation{}, errors.New("paper performance mark is ambiguous")
		}
		selected[bar.Symbol] = selectedMark{bar: bar, sequence: sequence}
	}
	if len(selected) != len(positions) {
		return nil, paperdomain.Valuation{}, ErrPaperRunnerIncompleteMarks
	}

	closes := make(map[string]string, len(selected))
	for symbol, mark := range selected {
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
		mark, exists := selected[symbol]
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
