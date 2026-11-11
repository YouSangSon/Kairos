package sqlite

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestFXObservationRecordReplayConflictAndExactSeries(t *testing.T) {
	svc, _ := conservationService(t, nil, nil)
	ctx := context.Background()
	first := FXObservationInput{
		Source: "local_fixture", SourceObservationID: "usdkrw_1", BaseCurrency: "USD", QuoteCurrency: "KRW",
		Rate: "1370.25", ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}
	stored, err := svc.RecordFX(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.RecordFX(ctx, first)
	if err != nil || !reflect.DeepEqual(stored, replayed) {
		t.Fatalf("exact replay drifted: stored=%+v replayed=%+v err=%v", stored, replayed, err)
	}
	changed := first
	changed.Rate = "1371"
	if _, err := svc.RecordFX(ctx, changed); err == nil {
		t.Fatal("observation identity was rebound to a different rate")
	}
	sameSlot := first
	sameSlot.SourceObservationID = "usdkrw_other"
	if _, err := svc.RecordFX(ctx, sameSlot); err == nil {
		t.Fatal("source/pair/time slot accepted two identities")
	}
	second := first
	second.SourceObservationID = "usdkrw_2"
	second.Rate = "1380"
	second.ObservedAt = "2026-01-10T15:01:00Z"
	second.FetchedAt = "2026-01-10T15:01:01Z"
	if _, err := svc.RecordFX(ctx, second); err != nil {
		t.Fatal(err)
	}

	observations, proof, err := replayFXObservations(ctx, svc.reader())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 2 || observations[0].SourceObservationID != first.SourceObservationID || observations[1].SourceObservationID != second.SourceObservationID ||
		observations[0].Rate != "1370.25" || observations[1].Rate != "1380" || proof.Observations != 2 || len(proof.SHA256) != 64 {
		t.Fatalf("FX series/proof drifted: observations=%+v proof=%+v", observations, proof)
	}
	snapshot, err := svc.ledger.Snapshot(ctx)
	if err != nil || snapshot.ValuationStatus != "unavailable" {
		t.Fatalf("storage leaf changed valuation authority: snapshot=%+v err=%v", snapshot, err)
	}
}

func TestFXObservationRejectsInvalidBoundaryAndDirectWrites(t *testing.T) {
	valid := FXObservationInput{
		Source: "local_fixture", SourceObservationID: "fx_obs_valid", BaseCurrency: "USD", QuoteCurrency: "KRW",
		Rate: "1370.25", ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}
	tests := map[string]func(*FXObservationInput){
		"empty id":       func(v *FXObservationInput) { v.SourceObservationID = "" },
		"unknown source": func(v *FXObservationInput) { v.Source = "kiwoom" },
		"lower currency": func(v *FXObservationInput) { v.BaseCurrency = "usd" },
		"same currency":  func(v *FXObservationInput) { v.QuoteCurrency = "USD" },
		"zero":           func(v *FXObservationInput) { v.Rate = "0" },
		"negative":       func(v *FXObservationInput) { v.Rate = "-1" },
		"exponent":       func(v *FXObservationInput) { v.Rate = "1e3" },
		"trailing zero":  func(v *FXObservationInput) { v.Rate = "1370.250" },
		"offset time":    func(v *FXObservationInput) { v.ObservedAt = "2026-01-11T00:00:00+09:00" },
		"offset fetch":   func(v *FXObservationInput) { v.FetchedAt = "2026-01-11T00:00:01+09:00" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _ := conservationService(t, nil, nil)
			observation := valid
			mutate(&observation)
			if _, err := svc.RecordFX(context.Background(), observation); err == nil {
				t.Fatalf("invalid observation was accepted: %+v", observation)
			}
		})
	}

	svc, _ := conservationService(t, nil, nil)
	for _, statement := range []string{
		`INSERT INTO fx_observations(observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at) VALUES('same','local_fixture','same','USD','USD','1','2026-01-10T15:00:00Z','2026-01-10T15:00:01Z','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','2026-01-10T15:00:02Z')`,
		`INSERT INTO fx_observations(observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at) VALUES('bad-rate','local_fixture','bad-rate','USD','KRW','1.0','2026-01-10T15:00:00Z','2026-01-10T15:00:01Z','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','2026-01-10T15:00:02Z')`,
		`INSERT INTO fx_observations(observation_id,source,source_observation_id,base_currency,quote_currency,rate,observed_at,fetched_at,record_sha256,recorded_at) VALUES('bad-time','local_fixture','bad-time','USD','KRW','1','2026-01-11T00:00:00+09:00','2026-01-10T15:00:01Z','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','2026-01-10T15:00:02Z')`,
	} {
		if _, err := svc.db.Exec(statement); err == nil {
			t.Fatalf("schema accepted invalid observation: %s", statement)
		}
	}
}

func TestFXObservationRecoveryDetectsCorruptionAndMutation(t *testing.T) {
	svc, _ := conservationService(t, nil, nil)
	observation := FXObservationInput{
		Source: "local_fixture", SourceObservationID: "fx_obs_corruption", BaseCurrency: "USD", QuoteCurrency: "KRW",
		Rate: "1370", ObservedAt: "2026-01-10T15:00:00Z", FetchedAt: "2026-01-10T15:00:01Z",
	}
	if _, err := svc.RecordFX(context.Background(), observation); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE fx_observations SET rate=rate`,
		`DELETE FROM fx_observations`,
	} {
		if _, err := svc.db.Exec(statement); err == nil {
			t.Fatalf("insert-only storage accepted mutation: %s", statement)
		}
	}
	if _, err := svc.db.Exec(`DROP TRIGGER fx_observations_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE fx_observations SET rate='1371'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := replayFXObservations(context.Background(), svc.reader()); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("corrupt FX observation was certified: %v", err)
	}
}
