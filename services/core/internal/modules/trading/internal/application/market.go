package application

import (
	"context"
	"errors"
	"fmt"
	"omni-folio/services/core/internal/modules/trading/internal/domain"
)

const paperMarketBarSchema = "paper-market-bar.v1"

func (s *UseCases) RecordPaperMarketBar(ctx context.Context, input PaperMarketBarObservation) (*PaperMarketBarObservation, error) {
	if s == nil || s.journal == nil || s.now == nil {
		return nil, errors.New("paper market data recorder is not configured")
	}
	now := s.now().UTC()
	bar := input
	bar.ObservationID = paperMarketBarObservationID(input.Source, input.SourceObservationID)
	bar.SchemaVersion = paperMarketBarSchema
	bar.RecordedAt = now.Format(canonicalPaperTimeLayout)
	var ok bool
	if bar.OpenAt, ok = canonicalPaperTime(bar.OpenAt); !ok {
		return nil, errors.New("paper market bar open time is invalid")
	}
	if bar.CloseAt, ok = canonicalPaperTime(bar.CloseAt); !ok {
		return nil, errors.New("paper market bar close time is invalid")
	}
	if bar.SourceAvailableAt, ok = canonicalPaperTime(bar.SourceAvailableAt); !ok {
		return nil, errors.New("paper market bar availability time is invalid")
	}
	if bar.FetchedAt, ok = canonicalPaperTime(bar.FetchedAt); !ok {
		return nil, errors.New("paper market bar fetch time is invalid")
	}
	if err := validatePaperMarketBar(bar); err != nil {
		return nil, err
	}
	if fetched, _ := parsePaperTime(bar.FetchedAt); fetched.After(now) {
		return nil, errors.New("paper market bar was fetched in the future")
	}
	return writeResult(ctx, s.journal, func(tx WriteScope) (*PaperMarketBarObservation, error) {
		if _, err := tx.Market().ProveMarketRecovery(ctx); err != nil {
			return nil, fmt.Errorf("paper market recovery: %w", err)
		}
		stored, err := RecordPaperMarketBarTx(ctx, tx, bar)
		if err != nil {
			return nil, err
		}
		return stored, nil
	})
}
func RecordPaperMarketBarTx(ctx context.Context, tx WriteScope, bar PaperMarketBarObservation) (*PaperMarketBarObservation, error) {
	if tx == nil {
		return nil, errors.New("paper market transaction is required")
	}
	existing, found, err := tx.Market().LoadMarketBarBySource(ctx, bar.Source, bar.SourceObservationID)
	if err != nil {
		return nil, err
	}
	if found {
		if !samePaperMarketBarInput(*existing, bar) {
			return nil, errors.New("paper market source observation conflicts with its recorded bar")
		}
		return existing, nil
	}
	return tx.Market().InsertMarketBar(ctx, bar)
}
func (s *UseCases) RecordPaperSignalEventTx(ctx context.Context, tx WriteScope, accountRef string, signal PaperSignal) (*PaperSignalEvent, error) {
	if s == nil || s.journal == nil || s.now == nil || tx == nil || !orderAlias(accountRef, "account") {
		return nil, errors.New("paper signal recorder is not configured")
	}
	now := s.now().UTC()
	var ok bool
	if signal.DataAsOf, ok = canonicalPaperTime(signal.DataAsOf); !ok {
		return nil, errors.New("paper signal data time is invalid")
	}
	if signal.GeneratedAt, ok = canonicalPaperTime(signal.GeneratedAt); !ok {
		return nil, errors.New("paper signal generation time is invalid")
	}
	if signal.ExpiresAt, ok = canonicalPaperTime(signal.ExpiresAt); !ok {
		return nil, errors.New("paper signal expiry time is invalid")
	}
	if err := validateCapitalizedPaperSignalShape(signal); err != nil {
		return nil, err
	}
	if _, err := tx.Recovery().ProvePaperAccountingRecovery(ctx); err != nil {
		return nil, fmt.Errorf("paper signal accounting recovery: %w", err)
	}
	existing, found, err := tx.Market().LoadSignal(ctx, accountRef, signal.SignalID)
	if err != nil {
		return nil, err
	}
	if found {
		if !samePaperSignalInput(*existing, signal) {
			return nil, errors.New("paper signal identity conflicts with its recorded event")
		}
		return existing, nil
	}
	if err := validateCapitalizedPaperSignal(signal, now); err != nil {
		return nil, err
	}
	policy, err := tx.Selections().LoadCurrentStrategyExecutionPolicy(ctx, signal.StrategyResultSHA256, signal.StrategySelectionEventID)
	if err != nil {
		return nil, err
	}
	session, found, err := tx.Paper().LoadPaperAccountingSession(ctx, accountRef)
	if err != nil {
		return nil, err
	}
	if !found || session.ExecutionPolicySHA256 != policy.SHA256 {
		return nil, errors.New("paper signal accounting policy does not match the account session")
	}
	if err := tx.Market().ValidatePromotionSignalCutoff(ctx, signal.StrategyResultSHA256, signal.StrategySelectionEventID, signal.DataAsOf); err != nil {
		return nil, err
	}
	bar, barSequence, err := tx.Market().LoadMarketBarByID(ctx, signal.SignalBarObservationID)
	if err != nil {
		return nil, err
	}
	if bar.Symbol != signal.Symbol || bar.InputDataSHA256 != signal.DataSHA256 || bar.CloseAt != signal.DataAsOf ||
		bar.Source != "paper_fixture" || bar.Venue != "KRX" || bar.Currency != "KRW" || bar.Interval != "1d" ||
		bar.Timezone != "Asia/Seoul" || bar.PriceAdjustment != "unspecified" {
		return nil, errors.New("paper signal bar metadata is invalid")
	}
	availableAt, _ := parsePaperTime(bar.SourceAvailableAt)
	generatedAt, _ := parsePaperTime(signal.GeneratedAt)
	if generatedAt.Before(availableAt) {
		return nil, errors.New("paper signal predates source availability")
	}
	cutoff, sameSeriesLatest, err := tx.Market().SignalCutoff(ctx, *bar)
	if err != nil {
		return nil, err
	}
	if barSequence != sameSeriesLatest {
		return nil, errors.New("paper signal bar is not latest at the transaction cutoff")
	}
	legacyOrders, err := tx.Market().LegacyOrderCount(ctx, accountRef)
	if err != nil {
		return nil, err
	}
	if legacyOrders != 0 {
		return nil, errors.New("paper signal cannot follow a legacy paper order")
	}
	event := PaperSignalEvent{
		EventID: paperSignalEventID(accountRef, signal.SignalID), SchemaVersion: capitalizedPaperSignalSchema,
		AccountRef: accountRef, PaperAccountingSessionID: session.SessionID,
		StrategyResultSHA256: signal.StrategyResultSHA256, StrategySelectionEventID: signal.StrategySelectionEventID,
		ExecutionPolicySHA256: policy.SHA256, SignalID: signal.SignalID, SignalBarObservationID: signal.SignalBarObservationID,
		DataSHA256: signal.DataSHA256, Symbol: signal.Symbol, TargetQuantity: signal.TargetQuantity,
		DataAsOf: signal.DataAsOf, GeneratedAt: signal.GeneratedAt, ExpiresAt: signal.ExpiresAt,
		MarketObservationSequenceCutoff: cutoff, RecordedAt: now.Format(canonicalPaperTimeLayout),
	}
	if err := validatePaperSignalEvent(event); err != nil {
		return nil, err
	}
	return tx.Market().InsertSignal(ctx, event)
}
func (s *UseCases) ImportPaperSnapshot(ctx context.Context, raw []byte) (*PaperSnapshotImport, error) {
	if s == nil || s.journal == nil || s.now == nil {
		return nil, errors.New("paper snapshot importer is not configured")
	}
	bars, inputSHA, err := s.inputs.Snapshot(raw, s.now)
	if err != nil {
		return nil, err
	}

	return writeResult(ctx, s.journal, func(tx WriteScope) (*PaperSnapshotImport, error) {
		if _, err := tx.Market().ProveMarketRecovery(ctx); err != nil {
			return nil, fmt.Errorf("paper market recovery: %w", err)
		}
		existing, latestClose, err := tx.Market().SnapshotRange(ctx, bars[0], bars[len(bars)-1])
		if err != nil {
			return nil, err
		}
		provided := make(map[string]bool, len(bars))
		for _, bar := range bars {
			provided[bar.CloseAt] = true
		}
		for closeAt := range existing {
			if !provided[closeAt] {
				return nil, errors.New("paper snapshot omits stored history inside its range")
			}
		}

		added := 0
		var anchor *PaperMarketBarObservation
		for index := range bars {
			bar := bars[index]
			matches := existing[bar.CloseAt]
			if len(matches) > 1 {
				return nil, errors.New("paper snapshot history is ambiguous")
			}
			if len(matches) == 1 {
				if !domain.SamePaperSnapshotBar(domain.PaperMarketBarObservation(matches[0]), domain.PaperMarketBarObservation(bar)) {
					return nil, errors.New("paper snapshot conflicts with stored history")
				}
				anchor = &matches[0]
				continue
			}
			if latestClose != "" && bar.CloseAt <= latestClose {
				return nil, errors.New("paper snapshot cannot insert retroactive history")
			}
			stored, err := RecordPaperMarketBarTx(ctx, tx, bar)
			if err != nil {
				return nil, err
			}
			anchor, added, latestClose = stored, added+1, bar.CloseAt
		}
		if anchor == nil {
			return nil, errors.New("paper snapshot has no anchor")
		}
		if anchor.InputDataSHA256 != inputSHA {
			return nil, errors.New("paper snapshot bytes conflict with the stored anchor")
		}
		return &PaperSnapshotImport{InputSHA256: inputSHA, SignalBarObservationID: anchor.ObservationID, Bars: len(bars), Added: added}, nil
	})
}
