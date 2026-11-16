package main

import (
	"context"
	"testing"
)

func recordG38C2FillBar(t testing.TB, svc *Service, sourceID, open, volume, day string) *PaperMarketBarObservation {
	return recordG38C2SymbolFillBar(t, svc, "005930", sourceID, open, volume, day)
}

func recordG38C2SymbolFillBar(t testing.TB, svc *Service, symbol, sourceID, open, volume, day string) *PaperMarketBarObservation {
	t.Helper()
	bar := g38c2PaperMarketBar(sourceID, day+"T00:00:00Z", day+"T06:30:00Z")
	bar.Symbol = symbol
	bar.Open, bar.High, bar.Low, bar.Close, bar.Volume = open, open, open, open, volume
	bar.SourceAvailableAt, bar.FetchedAt = day+"T06:31:00Z", day+"T06:32:00Z"
	recorded, err := svc.recordPaperMarketBar(context.Background(), bar)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}
