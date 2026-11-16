package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const ownerPriceTestInput = `{"schema_version":"owner-price-series.v1","source_version":"price-v1","instrument_id":"instrument_aapl","symbol":"AAPL","venue":"XNAS","currency":"USD","price_adjustment":"unadjusted","sample":false,"fetched_at":"2026-01-01T00:00:00Z","points":[{"observed_at":"2025-01-01T00:00:00Z","price":"100"},{"observed_at":"2026-01-01T00:00:00Z","price":"200"}]}`

func ownerPriceInputForTest(t testing.TB, change func(*OwnerPriceSeriesInput)) []byte {
	t.Helper()
	var input OwnerPriceSeriesInput
	if err := json.Unmarshal([]byte(ownerPriceTestInput), &input); err != nil {
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

func ownerPriceServiceForTest(t *testing.T) *conservationFixture {
	t.Helper()
	svc := ownerFXServiceForTest(t)
	applyTestCSV(t, svc, "owner-price-ledger", testCSV("capital,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,100", "buy,account-main,BUY,2025-01-02T00:00:00Z,AAPL,1,100,0,USD,-100"))
	return svc
}

func importOwnerPriceForTest(t testing.TB, svc *conservationFixture, raw []byte) *OwnerPriceSeriesReceipt {
	t.Helper()
	r, err := svc.ownerPrice.ImportOwnerPrice(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOwnerPriceCorrectionWithdrawalOldRetryAndSourcePin(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	first := importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput))
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00.1Z") }
	importOwnerPriceForTest(t, svc, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) {
		i.SourceVersion = "a-correction"
		i.Venue = "XNYS"
		i.Sample = true
		i.Points = i.Points[1:]
		i.Points[0].Price = "300"
	}))
	for _, test := range []struct {
		cutoff, version, price string
		count                  int
	}{{"2026-01-01T00:00:00Z", "price-v1", "200", 2}, {"2026-01-01T00:00:00.1Z", "a-correction", "300", 1}} {
		marks, err := valuationPriceObservations(context.Background(), svc.reader(), svc.admitted(t), priceMustTime(test.cutoff))
		if err != nil || len(marks) != test.count || marks[len(marks)-1].OwnerVersion != test.version || marks[len(marks)-1].Price != test.price {
			t.Fatalf("whole version drift: %+v %v", marks, err)
		}
	}
	importOwnerPriceForTest(t, svc, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.SourceVersion = "withdrawn"; i.Points = []OwnerPricePoint{} }))
	svc.now = func() time.Time { t.Fatal("old retry called clock"); return time.Time{} }
	if r := importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput)); !reflect.DeepEqual(r, first) {
		t.Fatal("old receipt changed")
	}
	marks, err := valuationPriceObservations(context.Background(), svc.reader(), svc.admitted(t), priceMustTime("2026-01-01T00:00:00.2Z"))
	if err != nil || len(marks) != 0 {
		t.Fatal("withdrawal or retry fell back")
	}
	svc.now = func() time.Time { return priceMustTime("2026-01-01T00:00:00.2Z") }
	if _, err := svc.RecordPrice(context.Background(), SecurityPriceObservationInput{Source: "local_fixture", SourceObservationID: "blocked", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "200", PriceAdjustment: "unspecified", ObservedAt: "2026-01-01T00:00:00Z", FetchedAt: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("withdrawal released price source")
	}
	if _, err := svc.ownerPrice.ImportOwnerPrice(context.Background(), ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Sample = true })); err == nil {
		t.Fatal("same version changed payload")
	}
}

func TestOwnerPriceRequestBindingAndFutureCapture(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	ctx := context.Background()
	if _, err := svc.ownerPrice.ImportOwnerPrice(ctx, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.FetchedAt = "2026-01-01T00:00:00.000000001Z" })); err == nil {
		t.Fatal("future capture accepted")
	}
}

func TestOwnerPriceSharedInputAndReceiptContract(t *testing.T) {
	schema := readJSONMap(t, filepath.Join(conservationContracts(), "owner-price-series.schema.json"))
	svc := ownerPriceServiceForTest(t)
	input, _, _, err := decodeOwnerPriceSeries(context.Background(), fixture(t, "owner-price-aapl-sample.json"), svc.ledger.CanonicalInstrumentID)
	if err != nil || !input.Sample || input.PriceAdjustment != "unadjusted" {
		t.Fatalf("shared price sample drift: %v", err)
	}
	receipt := importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput))
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
			t.Fatalf("%s schema/runtime fields differ", name)
		}
	}
}

func TestOwnerPricePhysicalAndTemporalProofFailsClosed(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	svc.db.SetMaxOpenConns(1)
	if _, err := svc.db.Exec(`CREATE TEMP TRIGGER owner_price_ignore BEFORE INSERT ON owner_price_series_versions BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ownerPrice.ImportOwnerPrice(context.Background(), []byte(ownerPriceTestInput)); err == nil {
		t.Fatal("extra ignore trigger accepted")
	}
}

func TestOwnerPriceRecoveryRequiresCanonicalLedgerMembership(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput))

	// A self-consistent rewrite still needs canonical ledger membership during recovery.
	var guards [2]string
	for n, name := range []string{"owner_price_series_versions_no_update", "owner_price_series_versions_insert_guard"} {
		if err := svc.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&guards[n]); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.db.Exec(`DROP TRIGGER ` + name); err != nil {
			t.Fatal(err)
		}
	}
	raw := ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Symbol = "MSFT"; i.InstrumentID = "instrument_msft" })
	i, canonical, sha, err := decodeOwnerPriceSeries(context.Background(), raw, svc.ledger.CanonicalInstrumentID)
	if err != nil {
		t.Fatal(err)
	}
	r := ownerPriceSeriesRecord{Sequence: 1, Input: *i, DataSHA256: sha, RecordedAt: "2026-01-01T00:00:00Z"}
	recordSHA, err := ownerPriceRecordSHA(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE owner_price_series_versions SET instrument_id=?,series_json=?,series_sha256=?,record_sha256=?`, i.InstrumentID, string(canonical), sha, recordSHA); err != nil {
		t.Fatal(err)
	}
	for _, guard := range guards {
		if _, err := svc.db.Exec(guard); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := proveOwnerPriceSeriesRecovery(context.Background(), svc.reader(), svc.admitted(t)); err == nil {
		t.Fatal("self-consistent phantom journal passed recovery")
	}
}
