package main

import (
	"context"
	"encoding/json"
	"testing"
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

func ownerPriceServiceForTest(t *testing.T) *Service {
	t.Helper()
	svc := ownerFXServiceForTest(t)
	applyTestCSV(t, svc, "owner-price-ledger", testCSV("capital,account-main,DEPOSIT,2025-01-01T00:00:00Z,,,,,USD,100", "buy,account-main,BUY,2025-01-02T00:00:00Z,AAPL,1,100,0,USD,-100"))
	return svc
}

func importOwnerPriceForTest(t testing.TB, svc *Service, raw []byte) *OwnerPriceSeriesReceipt {
	t.Helper()
	r, err := svc.importOwnerPriceSeries(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOwnerPriceLedgerMembershipAndMixedCaseHolding(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	if _, err := svc.importOwnerPriceSeries(context.Background(), ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Symbol = "MSFT"; i.InstrumentID = "instrument_msft" })); err == nil {
		t.Fatal("unadmitted instrument accepted")
	}
	if _, err := svc.db.Exec(`INSERT INTO events(event_id,source_event_id,account_id,type,occurred_at,instrument_id,symbol,quantity,price,fee,currency,amount,receipt_id,recorded_at) VALUES('legacy-case','legacy-case','account-main','BUY','2025-01-01T12:00:00Z','instrument_aapl','aapl','1','10','0','USD','-10','receipt-case','2026-01-01T00:00:00Z'); UPDATE ledger_meta SET revision=3,recorded_at='2026-01-01T00:00:00Z' WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput))
	value, appErr := svc.Portfolio.HoldingValuation(context.Background(), "2026-01-01T00:00:00Z")
	if appErr != nil || len(value.Lines) != 1 || value.Lines[0].Symbol != "aapl" || value.Lines[0].MarketValue == nil || *value.Lines[0].MarketValue != "400" || value.Sample || value.Status != "complete" {
		t.Fatalf("canonical mixed-case identity lost: %+v %v", value, appErr)
	}
}

func TestOwnerPriceSplitBoundaryAndIndependentSampleFlags(t *testing.T) {
	for _, test := range []struct{ at, status string }{{"2025-12-31T23:00:00Z", "unavailable"}, {"2025-12-31T23:00:00.000000001Z", "complete"}} {
		svc := ownerPriceServiceForTest(t)
		applyTestCSV(t, svc, "split", testCSV("split,account-main,SPLIT,2025-12-31T23:00:00Z,AAPL,2,,,USD,0"))
		importOwnerPriceForTest(t, svc, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.Points = []OwnerPricePoint{{ObservedAt: test.at, Price: "50"}} }))
		value, appErr := svc.Portfolio.HoldingValuation(context.Background(), "2026-01-01T00:00:00Z")
		if appErr != nil || value.Status != test.status || value.Sample || !hasIssueCode(value.Issues, "owner_declared_price") {
			t.Fatalf("split boundary drift: %+v %v", value, appErr)
		}
		if test.status == "unavailable" && (value.Lines[0].Price == nil || value.Lines[0].Issue.Code != "split_price_unavailable" || len(value.Totals) != 0) {
			t.Fatal("split failure hid source or exposed total")
		}
		importOwnerFXForTest(t, svc, []byte(ownerFXTestInput))
		current, appErr := svc.Portfolio.CurrentValuation(context.Background())
		if appErr != nil || current.Status != test.status || current.Sample || !hasIssueCode(current.Issues, "owner_declared_price") {
			t.Fatalf("current split boundary drift: %+v %v", current, appErr)
		}
		twr, appErr := svc.Portfolio.TWR(context.Background())
		if appErr != nil || (test.status == "complete" && twr.Status != "calculated") || (test.status == "unavailable" && twr.CumulativeRate != nil) || !hasIssueCode(twr.Issues, "owner_declared_price") {
			t.Fatalf("TWR split boundary drift: %+v %v", twr, appErr)
		}
	}
}

func TestOwnerPriceHistoricalCorrectionDoesNotFillFromOldVersion(t *testing.T) {
	svc := ownerPriceServiceForTest(t)
	applyTestCSV(t, svc, "later-capital", testCSV("later,account-main,DEPOSIT,2025-01-03T00:00:00Z,,,,,USD,10"))
	importOwnerFXForTest(t, svc, ownerFXInputForTest(t, func(i *OwnerFXSeriesInput) {
		i.Points = append(i.Points[:1], OwnerFXPoint{ObservedAt: "2025-01-03T00:00:00Z", Rate: "1000"}, i.Points[1])
	}))
	importOwnerPriceForTest(t, svc, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) {
		i.Points = append(i.Points[:1], OwnerPricePoint{ObservedAt: "2025-01-03T00:00:00Z", Price: "100"}, i.Points[1])
	}))
	if report, err := svc.Portfolio.TWR(context.Background()); err != nil || report.Status != "calculated" {
		t.Fatalf("complete historical version unavailable: %+v %v", report, err)
	}
	importOwnerPriceForTest(t, svc, ownerPriceInputForTest(t, func(i *OwnerPriceSeriesInput) { i.SourceVersion = "removed-history" }))
	current, err := svc.Portfolio.CurrentValuation(context.Background())
	if err != nil || current.Status != "complete" {
		t.Fatalf("terminal price unexpectedly removed: %+v %v", current, err)
	}
	report, err := svc.Portfolio.TWR(context.Background())
	if err != nil || report.Status != "unavailable" || report.CumulativeRate != nil || !hasIssueCode(report.Issues, "owner_declared_price") {
		t.Fatalf("old historical point silently filled: %+v %v", report, err)
	}
}

func TestOwnerPriceFixtureBindingAndInternalTimeNamespace(t *testing.T) {
	t.Run("fixture cannot become owner", func(t *testing.T) {
		svc := ownerPriceServiceForTest(t)
		if _, err := svc.recordSecurityPriceObservation(context.Background(), SecurityPriceObservationInput{Source: "local_fixture", SourceObservationID: "aapl", InstrumentID: "instrument_aapl", Symbol: "AAPL", Venue: "XNAS", Currency: "USD", Price: "100", PriceAdjustment: "unspecified", ObservedAt: "2026-01-01T00:00:00Z", FetchedAt: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.importOwnerPriceSeries(context.Background(), []byte(ownerPriceTestInput)); err == nil {
			t.Fatal("fixture source became owner")
		}
		proof, err := proveOwnerPriceSeriesRecovery(context.Background(), svc.db)
		if err != nil || proof.Versions != 0 {
			t.Fatal("failed admission changed journal")
		}
	})
	t.Run("public ID prefix is not a namespace", func(t *testing.T) {
		svc := ownerPriceServiceForTest(t)
		originalID := svc.id
		svc.id = func(prefix string) string {
			if prefix == "security_price_observation" {
				return "owner_price_1_0"
			}
			return originalID(prefix)
		}
		if _, err := svc.recordSecurityPriceObservation(context.Background(), SecurityPriceObservationInput{Source: "local_fixture", SourceObservationID: "msft", InstrumentID: "instrument_msft", Symbol: "MSFT", Venue: "XNAS", Currency: "USD", Price: "1", PriceAdjustment: "unspecified", ObservedAt: "2025-01-01T00:00:00.000000001Z", FetchedAt: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		importOwnerPriceForTest(t, svc, []byte(ownerPriceTestInput))
		checkOwnerFXHTTPGolden(t, svc, "/v1/portfolio/holding-valuation?as_of=2026-01-01T00:00:00Z", ownerPriceGoldenForTest(t)["holding"])
	})
}
