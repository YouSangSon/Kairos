package application

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

type PerformanceMarkReader interface {
	CashCloseAnchor(context.Context, int64, string, string) (int, error)
	MarkObservationIDs(context.Context, int64, string) ([]string, error)
	LoadMarkBar(context.Context, string) (*PaperMarketBarObservation, int64, error)
}

func DerivePaperPerformanceMarks(ctx context.Context, q PerformanceMarkReader, state PaperAccountState, startingCash, asOf, recordedAt string, marketCutoff int64) ([]PerformanceMark, paperdomain.Valuation, error) {
	selection, err := domain.NewPerformanceMarkSelection(state, asOf, recordedAt, marketCutoff)
	if err != nil {
		return nil, paperdomain.Valuation{}, err
	}
	if !selection.NeedsMarks() {
		anchored, err := q.CashCloseAnchor(ctx, marketCutoff, asOf, recordedAt)
		if err != nil {
			return nil, paperdomain.Valuation{}, err
		}
		return domain.CashOnlyPerformanceValuation(anchored, startingCash, state)
	}
	observationIDs, err := q.MarkObservationIDs(ctx, marketCutoff, asOf)
	if err != nil {
		return nil, paperdomain.Valuation{}, err
	}
	for _, observationID := range observationIDs {
		bar, sequence, err := q.LoadMarkBar(ctx, observationID)
		if err != nil {
			return nil, paperdomain.Valuation{}, err
		}
		if err := selection.Consider((*domain.PaperMarketBarObservation)(bar), sequence); err != nil {
			return nil, paperdomain.Valuation{}, err
		}
	}
	return selection.Value(startingCash, state)
}
