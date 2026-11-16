package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	market "omni-folio/services/core/internal/modules/marketdata/contracts"
)

func TestOwnerPriceImportClockOnlyRecordsNewVersions(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	at := mustTime("2026-01-01T00:00:01Z")
	calls := 0
	svc.now = func() time.Time { calls++; return at }
	stored, err := svc.importOwnerPriceSeries(context.Background(), []byte(ownerPriceTestInput))
	if err != nil || stored == nil || calls != 1 || stored.RecordedAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("new version receipt=%+v clock_calls=%d err=%v", stored, calls, err)
	}
	calls = 0
	replayed, err := svc.importOwnerPriceSeries(context.Background(), []byte(ownerPriceTestInput))
	if err != nil || !reflect.DeepEqual(replayed, stored) || calls != 0 {
		t.Fatalf("exact retry changed receipt or read clock: %+v calls=%d err=%v", replayed, calls, err)
	}
	changed := ownerPriceInputForTest(t, func(input *OwnerPriceSeriesInput) { input.Points[0].Price = "101" })
	if _, err := svc.importOwnerPriceSeries(context.Background(), changed); !errors.Is(err, market.ErrConflict) || calls != 0 {
		t.Fatalf("same-version conflict err=%v clock_calls=%d", err, calls)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM owner_price_series_versions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry or conflict changed version count: %d err=%v", count, err)
	}
}
