package main

import (
	"context"
	"testing"
	"time"
)

func g38c2PaperSignalFixture(t *testing.T, openSession bool) (*Service, PaperSignal, *PaperMarketBarObservation) {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-10T07:00:00Z") }
	ctx := context.Background()
	evidence, selected := selectedPaperStrategy(t, svc)
	if openSession {
		if _, err := svc.openPaperAccountingSession(ctx, k2aAccountRef, evidence.ResultSHA256, selected.CurrentEventID); err != nil {
			t.Fatal(err)
		}
	}
	bar, err := svc.recordPaperMarketBar(ctx, g38c2PaperMarketBar("fixture-005930-20260109", "2026-01-09T00:00:00Z", "2026-01-09T06:30:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	return svc, PaperSignal{
		SchemaVersion: "paper-signal.v3", SignalID: "g38c2-signal-001", SignalBarObservationID: bar.ObservationID,
		StrategyResultSHA256: evidence.ResultSHA256, StrategySelectionEventID: selected.CurrentEventID,
		DataSHA256: bar.InputDataSHA256, Symbol: bar.Symbol, TargetQuantity: "0", DataAsOf: bar.CloseAt,
		GeneratedAt: bar.SourceAvailableAt, ExpiresAt: "2026-01-11T00:00:00Z",
	}, bar
}

func legacyPaperOrderIntentForTest(signal PaperSignal) OrderIntent {
	return OrderIntent{
		ClientOrderID: "paper_" + signal.SignalID, Provider: "kiwoom", Mode: "paper", AccountRef: k2aAccountRef,
		Symbol: signal.Symbol, Exchange: "KRX", Side: "BUY", OrderType: "LIMIT", Quantity: "1", LimitPrice: "1000", Currency: "KRW",
		StrategyResultSHA256: signal.StrategyResultSHA256, StrategySelectionEventID: signal.StrategySelectionEventID,
		SignalSchemaVersion: signal.SchemaVersion, SignalID: signal.SignalID, SignalDataSHA256: signal.DataSHA256,
		SignalDataAsOf: signal.DataAsOf, SignalGeneratedAt: signal.GeneratedAt, SignalExpiresAt: signal.ExpiresAt, SignalTargetQuantity: signal.TargetQuantity,
	}
}
