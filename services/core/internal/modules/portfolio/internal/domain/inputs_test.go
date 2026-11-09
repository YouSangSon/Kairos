package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFixtureAndOwnerObservationIDsKeepSeparateTimes(t *testing.T) {
	fixtureAt := "2025-01-01T00:00:00.000000001Z"
	ownerAt := "2025-01-01T00:00:00Z"
	known := "2026-01-01T00:00:00Z"
	fx := []FXObservation{{ObservationID: "owner_fx_1_0", Source: "local_fixture", ObservedAt: fixtureAt, FetchedAt: known, RecordedAt: known}, {ObservationID: "owner_fx_1_0", Source: ownerFXSource, OwnerVersion: "manual-v1", ObservedAt: ownerAt, FetchedAt: known, RecordedAt: known}}
	times, err := currentFXObservationTimes(fx)
	if err != nil || len(times) != 2 || times["owner_fx_1_0"][0].Equal(times["\x00owner_fx_1_0"][0]) {
		t.Fatalf("FX namespace collision: %+v %v", times, err)
	}
	raw, err := json.Marshal(publicCashValuationFX(&fx[1]))
	if err != nil || strings.Contains(string(raw), `\u0000`) {
		t.Fatal("internal FX key leaked", err)
	}
	prices := []SecurityPriceObservation{{ObservationID: "owner_price_1_0", Source: "local_fixture", ObservedAt: fixtureAt, FetchedAt: known, RecordedAt: known}, {ObservationID: "owner_price_1_0", Source: ownerPriceSource, OwnerVersion: "price-v1", ObservedAt: ownerAt, FetchedAt: known, RecordedAt: known}}
	pt, err := currentPriceObservationTimes(prices)
	if err != nil || len(pt) != 2 || pt["owner_price_1_0"][0].Equal(pt["\x00owner_price_1_0"][0]) {
		t.Fatalf("price namespace collision: %+v %v", pt, err)
	}
	mark := prices[1]
	inputs := valuationPriceInputs{times: pt, lastSplits: map[string]time.Time{ownerPriceKey("", ""): pt[priceTimeKey(mark)][0]}}
	if inputs.afterLastSplit(&mark) {
		t.Fatal("equal-time split price accepted")
	}
	mark.ObservationID = "later"
	pt[priceTimeKey(mark)] = [3]time.Time{pt["\x00owner_price_1_0"][0].Add(time.Nanosecond)}
	if !inputs.afterLastSplit(&mark) {
		t.Fatal("strictly later split price rejected")
	}
}
