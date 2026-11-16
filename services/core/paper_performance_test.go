package main

import (
	"context"
	"testing"
	"time"
)

func g38c3CashPerformanceFixture(t *testing.T) (*Service, string) {
	t.Helper()
	svc, _, _ := g38c2PaperSignalFixture(t, true)
	svc.now = func() time.Time { return mustTime("2026-01-11T07:00:00Z") }
	asOf := "2026-01-11T06:30:00.000000000Z"
	recordG38C3MarkBar(t, svc, "005930", "g38c3-cash-anchor", asOf, "100")
	return svc, asOf
}

func recordG38C3MarkBar(t *testing.T, svc *Service, symbol, sourceID, closeAt, close string) *PaperMarketBarObservation {
	t.Helper()
	day := closeAt[:10]
	return recordG38C3MarkBarAt(t, svc, symbol, sourceID, day+"T00:00:00Z", closeAt, close)
}

func recordG38C3MarkBarAt(t *testing.T, svc *Service, symbol, sourceID, openAt, closeAt, close string) *PaperMarketBarObservation {
	t.Helper()
	day := closeAt[:10]
	bar := g38c2PaperMarketBar(sourceID, openAt, closeAt)
	bar.Symbol, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume = symbol, close, close, close, close, "100"
	bar.SourceAvailableAt, bar.FetchedAt = day+"T06:31:00Z", day+"T06:32:00Z"
	return recordPaperMarketBarForG38C3(t, svc, bar)
}

func recordPaperMarketBarForG38C3(t *testing.T, svc *Service, bar PaperMarketBarObservation) *PaperMarketBarObservation {
	t.Helper()
	recorded, err := svc.recordPaperMarketBar(context.Background(), bar)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}
