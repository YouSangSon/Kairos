package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
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

func importOwnerFXForTest(t testing.TB, svc *conservationFixture, raw []byte) *OwnerFXSeriesReceipt {
	t.Helper()
	r, err := svc.ImportOwnerFX(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ownerFXServiceForTest(t *testing.T) *conservationFixture {
	t.Helper()
	svc, _ := conservationService(t, nil, nil)
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00Z") }
	return svc
}

func TestOwnerFXWholeVersionCorrectionWithdrawalAndOldRetry(t *testing.T) {
	svc := ownerFXServiceForTest(t)
	first := importOwnerFXForTest(t, svc, []byte(ownerFXTestInput))
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00.1Z") }
	second := ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) {
		i.SourceVersion = "a-correction"
		i.Sample = true
		i.Points[1].Rate = "1200"
	})
	importOwnerFXForTest(t, svc, second)
	for _, test := range []struct {
		cutoff, rate, version string
		sample                bool
	}{
		{"2026-01-01T00:00:00Z", "900", "manual-v1", false},
		{"2026-01-01T00:00:00.1Z", "1200", "a-correction", true},
	} {
		marks, err := valuationFXObservations(context.Background(), svc.reader(), priceMustTime(test.cutoff))
		if err != nil || len(marks) != 2 || marks[1].Rate != test.rate || marks[1].OwnerVersion != test.version || marks[1].OwnerSample != test.sample {
			t.Fatalf("whole known version drift: %+v %v", marks, err)
		}
	}
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00.2Z") }
	withdrawal := ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.SourceVersion = "withdrawn"; i.Points = []OwnerFXPoint{} })
	cleared := importOwnerFXForTest(t, svc, withdrawal)
	if cleared.PointCount != 0 || cleared.FirstObservedAt != nil || cleared.LastObservedAt != nil {
		t.Fatal("withdrawal invented a mark")
	}
	svc.now = func() time.Time { t.Fatal("exact retry called clock"); return time.Time{} }
	if retry := importOwnerFXForTest(t, svc, []byte(ownerFXTestInput)); !reflect.DeepEqual(retry, first) {
		t.Fatal("old receipt changed")
	}
	marks, err := valuationFXObservations(context.Background(), svc.reader(), priceMustTime("2026-01-01T00:00:00.3Z"))
	if err != nil || len(marks) != 0 {
		t.Fatal("withdrawal or old retry fell back to prior points")
	}
	if _, err := svc.ImportOwnerFX(context.Background(), ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Sample = true })); err == nil {
		t.Fatal("same version changed sample declaration")
	}
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00.15Z") }
	if _, err := svc.ImportOwnerFX(context.Background(), ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.SourceVersion = "backwards" })); err == nil {
		t.Fatal("record clock moved backwards")
	}
	proof, err := proveOwnerFXSeriesRecovery(context.Background(), svc.reader())
	if err != nil || proof.Versions != 3 || proof.Points != 4 {
		t.Fatalf("rejected/retried input changed proof: %+v %v", proof, err)
	}
}

func TestOwnerFXPairBindingSurvivesWithdrawalAndIDsAreNamespaced(t *testing.T) {
	t.Run("fixture pair cannot become owner", func(t *testing.T) {
		svc := ownerFXServiceForTest(t)
		recordXIRRFX(t, svc, "fixture", "1000", "2025-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
		if _, err := svc.ImportOwnerFX(context.Background(), []byte(ownerFXTestInput)); err == nil {
			t.Fatal("mixed source accepted")
		}
		proof, err := proveOwnerFXSeriesRecovery(context.Background(), svc.reader())
		if err != nil || proof.Versions != 0 {
			t.Fatal("failed import appended")
		}
	})
	t.Run("withdrawn owner pair cannot become fixture", func(t *testing.T) {
		svc := ownerFXServiceForTest(t)
		importOwnerFXForTest(t, svc, ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.Points = []OwnerFXPoint{} }))
		if _, err := svc.RecordFX(context.Background(), FXObservationInput{Source: "local_fixture", SourceObservationID: "fixture", BaseCurrency: "USD", QuoteCurrency: "KRW", Rate: "900", ObservedAt: "2026-01-01T00:00:00Z", FetchedAt: "2026-01-01T00:00:00Z"}); err == nil {
			t.Fatal("withdrawal cleared source binding")
		}
	})
	t.Run("public ID collision cannot alias time", func(t *testing.T) {
		svc := ownerFXServiceForTest(t)
		originalID := svc.id
		svc.id = func(prefix string) string {
			if prefix == "fx_observation" {
				return "owner_fx_1_0"
			}
			return originalID(prefix)
		}
		if _, err := svc.RecordFX(context.Background(), FXObservationInput{Source: "local_fixture", SourceObservationID: "eur", BaseCurrency: "EUR", QuoteCurrency: "KRW", Rate: "1500", ObservedAt: "2025-01-01T00:00:00.000000001Z", FetchedAt: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		importOwnerFXForTest(t, svc, []byte(ownerFXTestInput))
	})
}

func TestOwnerFXFutureCaptureAndRequestBinding(t *testing.T) {
	svc := ownerFXServiceForTest(t)
	if _, err := svc.ImportOwnerFX(context.Background(), ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) { i.FetchedAt = "2026-01-01T00:00:00.000000001Z" })); err == nil {
		t.Fatal("capture newer than the server clock accepted")
	}
}

func TestOwnerFXSharedInputAndReceiptContract(t *testing.T) {
	schema := readJSONMap(t, filepath.Join(conservationContracts(), "owner-fx-series.schema.json"))
	input, _, _, err := decodeOwnerFXSeries(context.Background(), fixture(t, "owner-fx-usdkrw-sample.json"))
	if err != nil || input == nil || !input.Sample || input.BaseCurrency != "USD" || input.QuoteCurrency != "KRW" {
		t.Fatalf("shared owner sample drift: %v", err)
	}
	receipt := importOwnerFXForTest(t, ownerFXServiceForTest(t), []byte(ownerFXTestInput))
	for name, value := range map[string]any{"input": input, "receipt": receipt} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		definition := schema
		if name == "receipt" {
			definition = schema["$defs"].(map[string]any)["receipt"].(map[string]any)
		}
		if !reflect.DeepEqual(jsonKeySet(fields), jsonStringSet(definition["required"])) {
			t.Fatalf("%s contract fields differ", name)
		}
	}
}
