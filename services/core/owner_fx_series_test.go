package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const ownerFXTestInput = `{"schema_version":"owner-fx-series.v1","source_version":"manual-v1","base_currency":"USD","quote_currency":"KRW","sample":false,"fetched_at":"2026-01-01T00:00:00Z","points":[{"observed_at":"2025-01-01T00:00:00Z","rate":"1000"},{"observed_at":"2026-01-01T00:00:00Z","rate":"900"}]}`

func ownerFXInputForTest(t testing.TB, change func(*OwnerFXSeriesInput)) []byte {
	t.Helper()
	var input OwnerFXSeriesInput
	if err := json.Unmarshal([]byte(ownerFXTestInput), &input); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&input)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func importOwnerFXForTest(t testing.TB, svc *Service, raw []byte) *OwnerFXSeriesReceipt {
	t.Helper()
	r, err := svc.importOwnerFXSeries(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ownerFXServiceForTest(t *testing.T) *Service {
	t.Helper()
	svc, _ := testService(t, nil, nil)
	svc.now = func() time.Time { return mustTime("2026-01-01T00:00:00Z") }
	return svc
}

func TestOwnerFXSchemaInventoryAndLaterCorruptionFailClosed(t *testing.T) {
	for _, table := range []string{"owner_fx_series_versions", "fx_observations"} {
		t.Run(table, func(t *testing.T) {
			svc := ownerFXServiceForTest(t)
			svc.db.SetMaxOpenConns(1)
			if _, err := svc.db.Exec(`CREATE TEMP TRIGGER owner_test_ignore BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(IGNORE); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.importOwnerFXSeries(context.Background(), []byte(ownerFXTestInput)); err == nil {
				t.Fatal("extra connection-local trigger accepted")
			}
		})
	}
	svc := ownerFXServiceForTest(t)
	importOwnerFXForTest(t, svc, []byte(ownerFXTestInput))
	var guard string
	if err := svc.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='owner_fx_series_versions_no_update'`).Scan(&guard); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`DROP TRIGGER owner_fx_series_versions_no_update; UPDATE owner_fx_series_versions SET record_sha256=?`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(guard); err != nil {
		t.Fatal(err)
	}
	if r, appErr := svc.Portfolio.CurrentValuation(context.Background()); appErr == nil || portfolioError(appErr).Body.Message != "internal server error" || r != nil {
		t.Fatal("empty portfolio bypassed later journal proof")
	}
}
